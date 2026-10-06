package model

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Типы действий правила.
const (
	ActionLinkPremises     = "link_premises"      // привязать к помещению (лицевой счёт выбирается по типу банковского счёта)
	ActionLinkAccount      = "link_account"       // привязать к конкретному лицевому счёту
	ActionSetCategory      = "set_category"       // поставить категорию (платёж не за лицевой счёт)
	ActionAccountFromText  = "account_from_text"  // взять номер лицевого счёта из текста по регулярному выражению
	ActionPremisesFromText = "premises_from_text" // взять номер помещения из текста
	ActionLinkByOwner      = "link_by_owner"      // найти помещение по ФИО/ИНН плательщика среди собственников и плательщиков счетов
)

var RuleActionTypes = []string{ActionLinkPremises, ActionLinkAccount, ActionSetCategory, ActionAccountFromText, ActionPremisesFromText, ActionLinkByOwner}

// Поля платежа, по которым можно ставить условия.
var (
	RuleTextFields = []string{"payer_name", "payer_inn", "payer_account", "payer_bank", "purpose", "doc_number", "comment", "operation_type"}
	ruleTextOps    = []string{"contains", "not_contains", "equals", "starts_with", "regex"}
	ruleAmountOps  = []string{"equals", "gt", "lt", "between"}
)

const (
	maxRuleConditions = 20
	maxRuleValues     = 50
	maxRuleValueLen   = 500
)

// RuleCondition — одно условие. Значения объединяются по «или» (у not_contains: ни одно из значений не встречается).
type RuleCondition struct {
	Field        string   `json:"field"`
	Op           string   `json:"op"`
	Values       []string `json:"values"`
	IgnoreSpaces bool     `json:"ignore_spaces,omitempty"` // сравнивать без пробелов («0 0 0 0 5 0 0 1 0 7»)
}

// RuleAction — что сделать с платежом, подошедшим под условия.
type RuleAction struct {
	Type              string `json:"type"`
	PremisesID        *int64 `json:"premises_id,omitempty"`
	PersonalAccountID *int64 `json:"personal_account_id,omitempty"`
	CategoryID        *int64 `json:"category_id,omitempty"`
	Field             string `json:"field,omitempty"`   // поле-источник текста (по умолчанию purpose)
	Pattern           string `json:"pattern,omitempty"` // регулярное выражение с группой захвата
	IgnoreSpaces      bool   `json:"ignore_spaces,omitempty"`
	PremisesKind      string `json:"premises_kind,omitempty"` // для premises_from_text
	BuildingID        *int64 `json:"building_id,omitempty"`   // для premises_from_text: ограничить домом
}

type RuleConditions []RuleCondition

// Scan/Value: условия и действие хранятся в JSONB.
func (c *RuleConditions) Scan(src any) error { return scanJSON(src, c) }
func (c RuleConditions) Value() (driver.Value, error) {
	if c == nil {
		c = RuleConditions{}
	}
	return marshalJSON(c)
}
func (a *RuleAction) Scan(src any) error          { return scanJSON(src, a) }
func (a RuleAction) Value() (driver.Value, error) { return marshalJSON(a) }

func scanJSON(src any, dst any) error {
	switch v := src.(type) {
	case []byte:
		return json.Unmarshal(v, dst)
	case string:
		return json.Unmarshal([]byte(v), dst)
	case nil:
		return nil
	}
	return fmt.Errorf("cannot scan %T into JSON", src)
}

func marshalJSON(v any) (driver.Value, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return string(b), nil
}

// PaymentRule — правило определения лицевого счёта входящего платежа. Правила применяются по порядку (position),
// первое правило, которое определило платёж, останавливает обработку.
type PaymentRule struct {
	Meta
	Name       string         `json:"name" db:"name"`
	Position   int            `json:"position" db:"position"`
	Enabled    bool           `json:"enabled" db:"enabled"`
	Direction  string         `json:"direction" db:"direction"`
	MatchMode  string         `json:"match_mode" db:"match_mode"` // all — все условия, any — любое
	Conditions RuleConditions `json:"conditions" db:"conditions"`
	Action     RuleAction     `json:"action" db:"action"`
}

func (r *PaymentRule) SetDefaults() {
	if r.Direction == "" {
		r.Direction = "incoming"
	}
	if r.MatchMode == "" {
		r.MatchMode = "all"
	}
	if r.Conditions == nil {
		r.Conditions = RuleConditions{}
	}
}

func (r PaymentRule) Validate() error {
	if err := firstErr(
		required("name", r.Name),
		oneOf("direction", r.Direction, []string{"incoming"}),
		oneOf("match_mode", r.MatchMode, []string{"all", "any"}),
	); err != nil {
		return err
	}
	if len(r.Conditions) > maxRuleConditions {
		return invalid("conditions", "must contain at most %d items", maxRuleConditions)
	}
	for i, c := range r.Conditions {
		if err := c.validate(); err != nil {
			return invalid("conditions", "condition %d: %s", i+1, err)
		}
	}
	return r.Action.validate()
}

func (c RuleCondition) validate() error {
	if len(c.Values) == 0 || len(c.Values) > maxRuleValues {
		return fmt.Errorf("values: from 1 to %d required", maxRuleValues)
	}
	for _, v := range c.Values {
		if strings.TrimSpace(v) == "" || len(v) > maxRuleValueLen {
			return fmt.Errorf("values: must be non-empty and shorter than %d characters", maxRuleValueLen)
		}
	}
	switch {
	case slices.Contains(RuleTextFields, c.Field):
		if !slices.Contains(ruleTextOps, c.Op) {
			return fmt.Errorf("op must be one of: %s", strings.Join(ruleTextOps, ", "))
		}
		if c.Op == "regex" {
			for _, v := range c.Values {
				if _, err := regexp.Compile(v); err != nil {
					return fmt.Errorf("regex %q: %v", v, err)
				}
			}
		}
	case c.Field == "amount":
		if !slices.Contains(ruleAmountOps, c.Op) {
			return fmt.Errorf("op must be one of: %s", strings.Join(ruleAmountOps, ", "))
		}
		want := 1
		if c.Op == "between" {
			want = 2
		}
		if len(c.Values) != want {
			return fmt.Errorf("op %s needs %d value(s)", c.Op, want)
		}
		for _, v := range c.Values {
			if _, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), 64); err != nil {
				return fmt.Errorf("amount %q is not a number", v)
			}
		}
	case c.Field == "bank_account_id":
		if c.Op != "equals" {
			return fmt.Errorf("op must be equals")
		}
		for _, v := range c.Values {
			if _, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err != nil {
				return fmt.Errorf("bank_account_id %q is not an integer", v)
			}
		}
	default:
		return fmt.Errorf("field must be one of: %s, amount, bank_account_id", strings.Join(RuleTextFields, ", "))
	}
	return nil
}

func (a RuleAction) validate() error {
	if err := oneOf("action.type", a.Type, RuleActionTypes); err != nil {
		return err
	}
	switch a.Type {
	case ActionLinkPremises:
		if a.PremisesID == nil {
			return invalid("action.premises_id", "is required")
		}
	case ActionLinkAccount:
		if a.PersonalAccountID == nil {
			return invalid("action.personal_account_id", "is required")
		}
	case ActionSetCategory:
		if a.CategoryID == nil {
			return invalid("action.category_id", "is required")
		}
	case ActionAccountFromText, ActionPremisesFromText:
		if a.Field != "" && !slices.Contains(RuleTextFields, a.Field) {
			return invalid("action.field", "must be one of: %s", strings.Join(RuleTextFields, ", "))
		}
		if a.Type == ActionPremisesFromText {
			if err := oneOf("action.premises_kind", a.PremisesKind, PremisesKinds); err != nil {
				return err
			}
			if a.Pattern == "" {
				return invalid("action.pattern", "is required")
			}
		}
		if a.Pattern != "" {
			re, err := regexp.Compile(a.Pattern)
			if err != nil {
				return invalid("action.pattern", "%v", err)
			}
			if re.NumSubexp() < 1 {
				return invalid("action.pattern", "must contain a capture group")
			}
		}
	}
	return nil
}

// Фильтр списка правил.
type PaymentRuleFilter struct {
	Deleted bool
	Enabled *bool
}
