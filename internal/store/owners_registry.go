package store

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"dom-backend/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// OwnersRegistry собирает реестр собственников дома: помещения с площадью и действующими на дату собственниками.
type OwnersRegistry struct{ pool *pgxpool.Pool }

func NewOwnersRegistry(pool *pgxpool.Pool) *OwnersRegistry { return &OwnersRegistry{pool} }

type registryPremises struct {
	Kind     string   `db:"kind"`
	KindName *string  `db:"kind_name"`
	Number   string   `db:"number"`
	Area     *float64 `db:"total_area"`
	Cadastr  *string  `db:"cadastral_number"`
	Owners   []string `db:"owners"`
}

func (s *OwnersRegistry) Build(ctx context.Context, buildingID int64, day time.Time) (model.OwnersRegistry, error) {
	b, err := NewBuildings(s.pool).Get(ctx, buildingID)
	if err != nil {
		return model.OwnersRegistry{}, err
	}
	if b.DeletedAt != nil {
		return model.OwnersRegistry{}, &Error{ErrNotFound, "building not found"}
	}
	items, err := collect[registryPremises](s.pool.Query(ctx,
		`SELECT pr.kind, k.name AS kind_name, pr.number, pr.total_area, pr.cadastral_number,
		        ARRAY(SELECT COALESCE(concat_ws(' ', p.last_name, p.first_name, p.middle_name), le.name)
		              FROM ownerships o
		              LEFT JOIN persons p ON p.id = o.person_id
		              LEFT JOIN legal_entities le ON le.id = o.legal_entity_id
		              WHERE o.premises_id = pr.id AND o.deleted_at IS NULL
		                AND o.valid_from <= $2 AND (o.valid_to IS NULL OR o.valid_to >= $2)
		              ORDER BY o.id) AS owners
		 FROM premises pr LEFT JOIN premises_kinds k ON k.code = pr.kind
		 WHERE pr.building_id = $1 AND pr.deleted_at IS NULL`, buildingID, day))
	if err != nil {
		return model.OwnersRegistry{}, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		a, c := items[i], items[j]
		if ra, rc := registryKindRank(a.Kind), registryKindRank(c.Kind); ra != rc {
			return ra < rc
		}
		if a.Kind != c.Kind {
			return a.Kind < c.Kind
		}
		return naturalLess(a.Number, c.Number)
	})
	reg := model.OwnersRegistry{Address: b.Address, Day: day.Format("2006-01-02"), Rows: make([]model.OwnersRegistryRow, 0, len(items))}
	for _, it := range items {
		row := model.OwnersRegistryRow{Label: RegistryLabel(it.Kind, it.KindName, it.Number), Area: it.Area, Owners: it.Owners}
		if it.Cadastr != nil {
			row.Cadastral = *it.Cadastr
		}
		reg.Rows = append(reg.Rows, row)
	}
	return reg, nil
}

// Порядок как в бумажном реестре: квартиры, коммерческие, остальное, машиноместа.
func registryKindRank(kind string) int {
	switch kind {
	case "apartment":
		return 0
	case "commercial":
		return 1
	case "parking_space":
		return 3
	}
	return 2
}

// RegistryLabel — подпись помещения: «кв.001», «оф.003», «мм.140»; номер из цифр дополняется нулями до трёх знаков.
func RegistryLabel(kind string, kindName *string, number string) string {
	prefix := map[string]string{"apartment": "кв.", "commercial": "оф.", "parking_space": "мм."}[kind]
	if prefix == "" {
		prefix = strings.ToLower(kind) + "."
		if kindName != nil && strings.TrimSpace(*kindName) != "" {
			prefix = strings.ToLower(*kindName) + " "
		}
	}
	if n, err := strconv.Atoi(number); err == nil && n >= 0 && strconv.Itoa(n) == number {
		number = fmt.Sprintf("%03d", n)
	}
	return prefix + number
}

// naturalLess сравнивает номера помещений: сначала числовая часть («2» раньше «10»), потом текст («5» раньше «5А»).
func naturalLess(a, b string) bool {
	na, ra := leadingNumber(a)
	nb, rb := leadingNumber(b)
	if na != nb {
		return na < nb
	}
	return ra < rb
}

func leadingNumber(s string) (int, string) {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == 0 || utf8.RuneCountInString(s[:i]) > 9 {
		return 1 << 30, s // без числа в начале — в конец
	}
	n, _ := strconv.Atoi(s[:i])
	return n, s[i:]
}
