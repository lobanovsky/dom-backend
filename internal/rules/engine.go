package rules

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"dom-backend/internal/model"
)

// Payment — платёж в виде, пригодном для проверки условий.
type Payment struct {
	ID            int64
	BankAccountID int64
	BankSpecial   bool // платёж пришёл на специальный счёт (капремонт)
	Date          time.Time
	Amount        float64
	PayerName     string
	PayerINN      string
	PayerAccount  string
	PayerBank     string // БИК и название банка
	Purpose       string
	DocNumber     string
	Comment       string
	OperationType string
}

// Outcome — результат применения правил к платежу. Если ни одно правило не определило платёж, RuleID == 0,
// а Reason объясняет, почему.
type Outcome struct {
	RuleID     int64
	AccountID  *int64
	CategoryID *int64
	Reason     string
	weak       bool // причина «в тексте ничего не найдено»: уступает более содержательным причинам других правил
}

func (o Outcome) Resolved() bool { return o.RuleID != 0 }

type compiledRule struct {
	rule     model.PaymentRule
	conds    []compiledCond
	actionRe *regexp.Regexp
}

type compiledCond struct {
	model.RuleCondition
	res     []*regexp.Regexp
	numbers []float64
}

// Engine — скомпилированные правила и справочники. Безопасен для последовательного использования.
type Engine struct {
	rules []compiledRule
	idx   *Index
}

// New компилирует включённые правила в порядке следования. Правила с неверным выражением пропускаются
// и возвращаются в errs (в БД такие не попадают: Validate их отсекает).
func New(rules []model.PaymentRule, idx *Index) (e *Engine, errs []error) {
	e = &Engine{idx: idx}
	for _, r := range rules {
		if !r.Enabled || r.DeletedAt != nil {
			continue
		}
		cr, err := compile(r)
		if err != nil {
			errs = append(errs, fmt.Errorf("rule %d %q: %w", r.ID, r.Name, err))
			continue
		}
		e.rules = append(e.rules, cr)
	}
	return e, errs
}

func compile(r model.PaymentRule) (compiledRule, error) {
	cr := compiledRule{rule: r}
	for _, c := range r.Conditions {
		cc := compiledCond{RuleCondition: c}
		switch {
		case c.Field == "amount":
			for _, v := range c.Values {
				f, err := strconv.ParseFloat(strings.ReplaceAll(strings.TrimSpace(v), ",", "."), 64)
				if err != nil {
					return cr, err
				}
				cc.numbers = append(cc.numbers, f)
			}
		case c.Op == "regex":
			for _, v := range c.Values {
				re, err := regexp.Compile("(?i)" + v)
				if err != nil {
					return cr, err
				}
				cc.res = append(cc.res, re)
			}
		}
		cr.conds = append(cr.conds, cc)
	}
	if r.Action.Pattern != "" {
		re, err := regexp.Compile("(?i)" + r.Action.Pattern)
		if err != nil {
			return cr, err
		}
		cr.actionRe = re
	}
	return cr, nil
}

// Resolve применяет правила по порядку; первое правило, определившее платёж, побеждает. Правило, у которого условия
// подошли, но действие не удалось выполнить (номер не найден, помещение неоднозначно), не останавливает обработку:
// его причина запоминается на случай, если платёж так и не будет определён.
func (e *Engine) Resolve(p Payment) Outcome {
	var strong, weak string
	for _, cr := range e.rules {
		if !cr.matches(p) {
			continue
		}
		out := cr.apply(e.idx, p)
		if out.Resolved() {
			return out
		}
		switch {
		case out.weak && weak == "":
			weak = out.Reason
		case !out.weak && strong == "":
			strong = out.Reason
		}
	}
	reason := strong
	if reason == "" {
		reason = weak
	}
	if reason == "" {
		reason = "не подошло ни одно правило"
	}
	return Outcome{Reason: reason}
}

func (cr compiledRule) matches(p Payment) bool {
	if len(cr.conds) == 0 {
		return true
	}
	any := cr.rule.MatchMode == "any"
	for _, c := range cr.conds {
		ok := c.matches(p)
		if any && ok {
			return true
		}
		if !any && !ok {
			return false
		}
	}
	return !any
}

func (c compiledCond) matches(p Payment) bool {
	switch c.Field {
	case "amount":
		return c.matchAmount(p.Amount)
	case "bank_account_id":
		for _, v := range c.Values {
			if id, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil && id == p.BankAccountID {
				return true
			}
		}
		return false
	}
	value := fieldText(p, c.Field)
	prep := NormText
	if c.IgnoreSpaces {
		prep = func(s string) string { return noSpaces(NormText(s)) }
	}
	hay := prep(value)
	switch c.Op {
	case "regex":
		for _, re := range c.res {
			if re.MatchString(value) {
				return true
			}
		}
		return false
	case "not_contains":
		for _, v := range c.Values {
			if strings.Contains(hay, prep(v)) {
				return false
			}
		}
		return true
	}
	for _, v := range c.Values {
		want := prep(v)
		switch c.Op {
		case "contains":
			if strings.Contains(hay, want) {
				return true
			}
		case "equals":
			if hay == want {
				return true
			}
		case "starts_with":
			if strings.HasPrefix(hay, want) {
				return true
			}
		}
	}
	return false
}

func (c compiledCond) matchAmount(a float64) bool {
	const eps = 0.004
	switch c.Op {
	case "equals":
		return a > c.numbers[0]-eps && a < c.numbers[0]+eps
	case "gt":
		return a > c.numbers[0]
	case "lt":
		return a < c.numbers[0]
	case "between":
		lo, hi := c.numbers[0], c.numbers[1]
		return a >= lo-eps && a <= hi+eps
	}
	return false
}

func fieldText(p Payment, field string) string {
	switch field {
	case "payer_name":
		return p.PayerName
	case "payer_inn":
		return p.PayerINN
	case "payer_account":
		return p.PayerAccount
	case "payer_bank":
		return p.PayerBank
	case "purpose":
		return p.Purpose
	case "doc_number":
		return p.DocNumber
	case "comment":
		return p.Comment
	case "operation_type":
		return p.OperationType
	}
	return ""
}

func (cr compiledRule) apply(idx *Index, p Payment) Outcome {
	a := cr.rule.Action
	id := cr.rule.ID
	switch a.Type {
	case model.ActionSetCategory:
		return Outcome{RuleID: id, CategoryID: a.CategoryID}
	case model.ActionLinkAccount:
		if a.PersonalAccountID == nil || !idx.HasAccount(*a.PersonalAccountID) {
			return Outcome{Reason: "лицевой счёт из правила не найден или удалён"}
		}
		return Outcome{RuleID: id, AccountID: a.PersonalAccountID}
	case model.ActionLinkPremises:
		acc, reason := idx.AccountOfPremises(derefInt(a.PremisesID), p.BankSpecial)
		if acc == nil {
			return Outcome{Reason: reason}
		}
		return Outcome{RuleID: id, AccountID: acc}
	case model.ActionAccountFromText:
		text := cr.sourceText(p)
		m := cr.actionRe.FindStringSubmatch(text)
		if m == nil {
			return Outcome{Reason: "номер лицевого счёта в тексте не найден", weak: true}
		}
		acc, ok := idx.AccountsByNumber[m[1]]
		if !ok {
			return Outcome{Reason: "лицевой счёт " + m[1] + " из текста не найден в системе"}
		}
		return Outcome{RuleID: id, AccountID: &acc.ID}
	case model.ActionPremisesFromText:
		m := cr.actionRe.FindStringSubmatch(cr.sourceText(p))
		if m == nil {
			return Outcome{Reason: "номер помещения в тексте не найден", weak: true}
		}
		pid, reason := idx.PremisesByNumber(a.PremisesKind, m[1], a.BuildingID)
		if pid == 0 {
			return Outcome{Reason: reason}
		}
		acc, reason := idx.AccountOfPremises(pid, p.BankSpecial)
		if acc == nil {
			return Outcome{Reason: reason}
		}
		return Outcome{RuleID: id, AccountID: acc}
	case model.ActionLinkByOwner:
		pid, reason := idx.PremisesByPayer(p)
		if pid == 0 {
			return Outcome{Reason: reason}
		}
		acc, reason := idx.AccountOfPremises(pid, p.BankSpecial)
		if acc == nil {
			return Outcome{Reason: reason}
		}
		return Outcome{RuleID: id, AccountID: acc}
	}
	return Outcome{Reason: "неизвестное действие правила"}
}

func (cr compiledRule) sourceText(p Payment) string {
	field := cr.rule.Action.Field
	if field == "" {
		field = "purpose"
	}
	text := fieldText(p, field)
	if cr.rule.Action.IgnoreSpaces {
		text = noSpaces(text)
	}
	return text
}

func derefInt(v *int64) int64 {
	if v == nil {
		return 0
	}
	return *v
}
