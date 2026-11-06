package store

import (
	"context"
	"sort"
	"time"

	"dom-backend/internal/model"

	"github.com/jackc/pgx/v5/pgxpool"
)

// PersonsReport собирает отчёт «физлица и их недвижимость»: действующие на дату владения физлиц.
type PersonsReport struct{ pool *pgxpool.Pool }

func NewPersonsReport(pool *pgxpool.Pool) *PersonsReport { return &PersonsReport{pool} }

type personsReportRow struct {
	PersonID int64    `db:"person_id"`
	Name     string   `db:"name"`
	Phones   []string `db:"phones"`
	Emails   []string `db:"emails"`
	Kind     string   `db:"kind"`
	KindName *string  `db:"kind_name"`
	Number   string   `db:"number"`
	Area     *float64 `db:"total_area"`
	Cadastr  *string  `db:"cadastral_number"`
	Address  string   `db:"address"`
	ShareNum int      `db:"share_num"`
	ShareDen int      `db:"share_den"`
}

func (s *PersonsReport) Build(ctx context.Context, day time.Time) (model.PersonsReport, error) {
	rows, err := collect[personsReportRow](s.pool.Query(ctx,
		`SELECT pe.id AS person_id, concat_ws(' ', pe.last_name, pe.first_name, pe.middle_name) AS name, pe.phones, pe.emails,
		        pr.kind, k.name AS kind_name, pr.number, pr.total_area, pr.cadastral_number, b.address, o.share_num, o.share_den
		 FROM ownerships o
		 JOIN persons pe ON pe.id = o.person_id AND pe.deleted_at IS NULL
		 JOIN premises pr ON pr.id = o.premises_id AND pr.deleted_at IS NULL
		 JOIN buildings b ON b.id = pr.building_id AND b.deleted_at IS NULL
		 LEFT JOIN premises_kinds k ON k.code = pr.kind
		 WHERE o.deleted_at IS NULL AND o.person_id IS NOT NULL
		   AND o.valid_from <= $1 AND (o.valid_to IS NULL OR o.valid_to >= $1)`, day))
	if err != nil {
		return model.PersonsReport{}, err
	}
	// Объекты в порядке «квартиры → коммерческие → прочее → машиноместа», номера по возрастанию.
	less := func(a, c personsReportRow) bool {
		if ra, rc := registryKindRank(a.Kind), registryKindRank(c.Kind); ra != rc {
			return ra < rc
		}
		if a.Kind != c.Kind {
			return a.Kind < c.Kind
		}
		if a.Address != c.Address {
			return a.Address < c.Address
		}
		return naturalLess(a.Number, c.Number)
	}
	sort.SliceStable(rows, func(i, j int) bool { return less(rows[i], rows[j]) })

	rep := model.PersonsReport{Day: day.Format("2006-01-02"), Persons: []model.PersonsReportPerson{}}
	index := map[int64]int{} // person_id -> позиция; люди идут по первому своему объекту
	addresses := map[string]bool{}
	for _, r := range rows {
		i, ok := index[r.PersonID]
		if !ok {
			i = len(rep.Persons)
			index[r.PersonID] = i
			rep.Persons = append(rep.Persons, model.PersonsReportPerson{Name: r.Name, Phones: r.Phones, Emails: r.Emails})
		}
		item := model.PersonsReportItem{Label: RegistryLabel(r.Kind, r.KindName, r.Number), Address: r.Address, Area: r.Area, ShareNum: r.ShareNum, ShareDen: r.ShareDen}
		if r.Cadastr != nil {
			item.Cadastral = *r.Cadastr
		}
		rep.Persons[i].Items = append(rep.Persons[i].Items, item)
		addresses[r.Address] = true
	}
	rep.MultipleBuildings = len(addresses) > 1
	return rep, nil
}
