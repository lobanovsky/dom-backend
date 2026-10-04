package store

import (
	"context"
	"os"
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
	cleanup(t, orgs.Delete, org.ID)
	b, err := buildings.Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "store-test"})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, buildings.Delete, b.ID)
	pr, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1", TotalArea: ptr(50.5)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, premises.Delete, pr.ID)
	person, err := persons.Create(ctx, model.Person{LastName: "Тестов", FirstName: "Тест", BirthDate: ptr(date(1980, 5, 1))})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, persons.Delete, person.ID)

	// поиск по подстроке ФИО без учёта регистра
	found, err := persons.List(ctx, model.PersonFilter{Q: ptr("тестов т")}, 50, 0)
	if err != nil || !containsPerson(found, person.ID) {
		t.Errorf("search q: found = %+v, err = %v", found, err)
	}
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
	cleanup(t, owns.Delete, o1.ID)
	o2, err := owns.Create(ctx, newOwn(1, 2, date(2021, 1, 1)))
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, owns.Delete, o2.ID)

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
	cleanup(t, owns.Delete, o3.ID)

	view, err := owns.ListByPremises(ctx, pr.ID)
	if err != nil || len(view) != 3 || view[0].OwnerName != "Тестов Тест" || view[0].OwnerKind != "person" {
		t.Errorf("ListByPremises = %+v, err = %v", view, err)
	}

	acc, err := accounts.Create(ctx, model.Account{Number: "store-test-1", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2020, 1, 1)})
	if err != nil {
		t.Fatal(err)
	}
	cleanup(t, accounts.Delete, acc.ID)
	if _, err := accounts.Get(ctx, acc.ID+1000000); !isKind(err, ErrNotFound) {
		t.Errorf("get missing: err = %v, want not found", err)
	}

	// удаление помещения с зависимостями запрещено
	if err := premises.Delete(ctx, pr.ID); !isKind(err, ErrInvalid) {
		t.Errorf("delete with dependents: err = %v, want invalid", err)
	}
}

// cleanup удаляет запись в конце теста и сообщает, если удалить не удалось.
func cleanup(t *testing.T, del func(context.Context, int64) error, id int64) {
	t.Helper()
	t.Cleanup(func() {
		if err := del(context.Background(), id); err != nil {
			t.Errorf("cleanup id %d: %v", id, err)
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
