package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/db"
	"dom-backend/internal/model"
)

func ptr[T any](v T) *T { return &v }

// testPool подключается к БД из TEST_DATABASE_URL; без неё тест пропускается.
// Тест создаёт свои записи и удаляет их в конце, чужие данные не трогает.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	if err := db.Migrate(url); err != nil {
		t.Fatal(err)
	}
	pool, err := db.NewPool(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func date(y int, m time.Month, d int) model.Date { return model.NewDate(y, m, d) }

func TestOwnershipsAndPremises(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	orgs, buildings, premises := NewOrganizations(pool), NewBuildings(pool), NewPremisesStore(pool)
	persons, owns, accounts := NewPersons(pool), NewOwnerships(pool), NewAccounts(pool)

	org, err := orgs.Create(ctx, model.Organization{Kind: "tsn", Name: "store-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := buildings.Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "store-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)
	pr, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1", TotalArea: ptr(50.5)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "premises", pr.ID)
	person, err := persons.Create(ctx, model.Person{
		LastName: "Тестов", FirstName: "Тест", BirthDate: ptr(date(1980, 5, 1)),
		Phones: []string{"+7 900 123 45 67", "+7 495 765 43 21"}, Emails: []string{"Test.Person@Example.com"},
	})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "persons", person.ID)

	// поиск по подстроке ФИО без учёта регистра
	found, err := persons.List(ctx, model.PersonFilter{Q: ptr("тестов т")}, 50, 0)
	if err != nil || !containsPerson(found, person.ID) {
		t.Errorf("search q: found = %+v, err = %v", found, err)
	}
	if len(person.Phones) != 2 || person.Phones[1] != "+7 495 765 43 21" || len(person.Emails) != 1 {
		t.Errorf("contacts round trip: phones = %q, emails = %q", person.Phones, person.Emails)
	}
	// поиск по любому из телефонов, по email, точный фильтр по телефону
	for name, f := range map[string]model.PersonFilter{
		"second phone": {Q: ptr("765 43")},
		"email":        {Q: ptr("test.person@example")},
		"exact phone":  {Phone: ptr("+7 495 765 43 21")},
	} {
		found, err = persons.List(ctx, f, 50, 0)
		if err != nil || !containsPerson(found, person.ID) {
			t.Errorf("search by %s: found = %+v, err = %v", name, found, err)
		}
	}
	found, err = persons.List(ctx, model.PersonFilter{Phone: ptr("+7 495")}, 50, 0)
	if err != nil || containsPerson(found, person.ID) {
		t.Errorf("exact phone filter must not match a prefix: found = %+v, err = %v", found, err)
	}
	// пустые списки сохраняются как пустые массивы, а не NULL
	bare, err := persons.Create(ctx, model.Person{LastName: "Безконтактов", FirstName: "Тест", Phones: []string{}, Emails: []string{}})
	if err != nil || bare.Phones == nil || len(bare.Phones) != 0 {
		t.Errorf("person without contacts: %+v, err = %v", bare, err)
	}
	cleanup(t, pool, "persons", bare.ID)

	found, err = persons.List(ctx, model.PersonFilter{Q: ptr("%")}, 50, 0)
	if err != nil || containsPerson(found, person.ID) {
		t.Errorf("search %%: must be literal, found = %+v, err = %v", found, err)
	}

	if pr.TotalArea == nil || *pr.TotalArea != 50.5 {
		t.Errorf("total_area round trip = %v", pr.TotalArea)
	}
	if person.BirthDate == nil || person.BirthDate.Format("2006-01-02") != "1980-05-01" {
		t.Errorf("birth_date round trip = %v", person.BirthDate)
	}

	// дубликат помещения -> ErrConflict
	_, err = premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	if !isKind(err, ErrConflict) {
		t.Errorf("duplicate premises: err = %v, want conflict", err)
	}

	newOwn := func(num, den int, from model.Date) model.Ownership {
		return model.Ownership{PremisesID: pr.ID, PersonID: &person.ID, ShareNum: num, ShareDen: den, ValidFrom: from}
	}
	o1, err := owns.Create(ctx, newOwn(1, 2, date(2020, 1, 1)))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "ownerships", o1.ID)
	o2, err := owns.Create(ctx, newOwn(1, 2, date(2021, 1, 1)))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "ownerships", o2.ID)

	// сумма долей > 1 -> ErrInvalid, запись откатывается
	if _, err = owns.Create(ctx, newOwn(1, 3, date(2022, 1, 1))); !isKind(err, ErrInvalid) {
		t.Errorf("shares > 1: err = %v, want invalid", err)
	}
	list, err := owns.List(ctx, model.OwnershipFilter{PremisesID: &pr.ID}, 50, 0)
	if err != nil || len(list) != 2 {
		t.Errorf("list after rollback: len = %d, err = %v, want 2", len(list), err)
	}

	// закрыли первое владение -> третье допустимо
	o1.ValidTo = ptr(date(2021, 12, 31))
	if _, err = owns.Update(ctx, o1.ID, o1); err != nil {
		t.Fatal(err)
	}
	o3, err := owns.Create(ctx, newOwn(1, 2, date(2022, 1, 1)))
	if err != nil {
		t.Fatalf("share after closed ownership: %v", err)
	}
	cleanup(t, pool, "ownerships", o3.ID)

	view, err := owns.ListByPremises(ctx, pr.ID, false)
	if err != nil || len(view) != 3 || view[0].OwnerName != "Тестов Тест" || view[0].OwnerKind != "person" {
		t.Errorf("ListByPremises = %+v, err = %v", view, err)
	}

	acc, err := accounts.Create(ctx, model.Account{Number: "store-test-1", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "personal_accounts", acc.ID)
	if _, err := accounts.Get(ctx, acc.ID+1000000); !isKind(err, ErrNotFound) {
		t.Errorf("get missing: err = %v, want not found", err)
	}

	// удаление помещения с действующими зависимостями запрещено
	if err := premises.Delete(ctx, pr.ID); !isKind(err, ErrInvalid) {
		t.Errorf("delete with dependents: err = %v, want invalid", err)
	}
}

// cleanup физически удаляет тестовую запись в конце теста (Delete хранилищ мягкий и
// оставил бы в БД мусор). Таблицы идут в обратном порядке создания благодаря LIFO t.Cleanup.
func cleanup(t *testing.T, pool *pgxpool.Pool, table string, id int64) {
	t.Helper()
	t.Cleanup(func() {
		if _, err := pool.Exec(context.Background(), fmt.Sprintf(`DELETE FROM %s WHERE id = $1`, table), id); err != nil {
			t.Errorf("cleanup %s %d: %v", table, id, err)
		}
	})
}

func containsPerson(items []model.Person, id int64) bool {
	for _, p := range items {
		if p.ID == id {
			return true
		}
	}
	return false
}

func isKind(err error, kind error) bool {
	e, ok := err.(*Error)
	return ok && e.Kind == kind
}

func TestImport(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	org, err := NewOrganizations(pool).Create(ctx, model.Organization{Kind: "tsn", Name: "import-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "organizations", org.ID)
	b, err := NewBuildings(pool).Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "import-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, pool, "buildings", b.ID)

	imp := NewImporter(pool)
	rows := []model.ImportRow{
		{Row: 2, Number: "1", Area: 50, CadastralNumber: "imp:1", LastName: "Импортов", FirstName: "Иван", MiddleName: "Иванович", UtilitiesAccount: "imp-u1", CapitalRepairAccount: "imp-k1"},
		{Row: 3, Number: "2", Area: 40.5, LastName: "импортов", FirstName: "иван", MiddleName: "иванович", UtilitiesAccount: "imp-u2", CapitalRepairAccount: "imp-k2"},
	}
	var created []int64
	t.Cleanup(func() {
		// порядок: дети раньше родителей; персоны, созданные импортом, помечены фамилией
		for _, q := range []string{
			`DELETE FROM account_holders WHERE account_id IN (SELECT id FROM personal_accounts WHERE number LIKE 'imp-%')`,
			`DELETE FROM personal_accounts WHERE number LIKE 'imp-%'`,
			`DELETE FROM ownerships WHERE premises_id IN (SELECT id FROM premises WHERE building_id = $1)`,
			`DELETE FROM premises WHERE building_id = $1`,
			`DELETE FROM persons WHERE last_name = 'Импортов'`,
		} {
			args := []any{}
			if strings.Contains(q, "$1") {
				args = append(args, b.ID)
			}
			if _, err := pool.Exec(ctx, q, args...); err != nil {
				t.Error(err)
			}
		}
		_ = created
	})

	res, err := imp.Import(ctx, b.ID, "apartment", rows)
	if err != nil {
		t.Fatal(err)
	}
	want := model.ImportResult{Premises: 2, Accounts: 4, Ownerships: 2, PersonsCreated: 1, PersonsReused: 1}
	if res != want {
		t.Fatalf("result = %+v, want %+v", res, want)
	}

	var personID int64
	if err := pool.QueryRow(ctx, `SELECT id FROM persons WHERE last_name = 'Импортов' AND deleted_at IS NULL`).Scan(&personID); err != nil {
		t.Fatal(err)
	}
	owned, err := NewProperties(pool).ByPerson(ctx, personID)
	if err != nil || len(owned) != 2 || owned[0].PremisesNumber != "1" || owned[1].PremisesNumber != "2" || owned[0].BuildingID != b.ID {
		t.Fatalf("ByPerson = %+v, err = %v", owned, err)
	}

	// повторный импорт отклоняется целиком, строка указана в сообщении
	_, err = imp.Import(ctx, b.ID, "apartment", rows[:1])
	var se *Error
	if !errors.As(err, &se) || !errors.Is(se.Kind, ErrConflict) || !strings.HasPrefix(se.Msg, "row 2:") {
		t.Fatalf("repeat import err = %v", err)
	}
	if _, err := imp.Import(ctx, 1<<40, "apartment", rows); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing building err = %v", err)
	}
}
