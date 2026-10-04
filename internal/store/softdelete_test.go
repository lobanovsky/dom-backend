package store

import (
	"context"
	"strings"
	"testing"

	"dom-backend/internal/model"
)

func errText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// Мягкое удаление: отметка вместо DELETE, защита связей триггерами БД,
// восстановление, повторное использование номера и учёт долей.
func TestSoftDelete(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	orgs, buildings, premises := NewOrganizations(pool), NewBuildings(pool), NewPremisesStore(pool)
	persons, owns, accounts := NewPersons(pool), NewOwnerships(pool), NewAccounts(pool)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}

	org, err := orgs.Create(ctx, model.Organization{Kind: "tsn", Name: "softdelete-test"})
	must(err)
	cleanup(t, pool, "organizations", org.ID)
	b, err := buildings.Create(ctx, model.Building{OrganizationID: org.ID, Kind: "apartment_building", Address: "softdelete-test"})
	must(err)
	cleanup(t, pool, "buildings", b.ID)
	pr, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "1"})
	must(err)
	cleanup(t, pool, "premises", pr.ID)
	person, err := persons.Create(ctx, model.Person{LastName: "Удалённов", FirstName: "Тест", Phones: []string{}, Emails: []string{}})
	must(err)
	cleanup(t, pool, "persons", person.ID)

	// --- запрет удаления при действующих связях ---
	if err := buildings.Delete(ctx, b.ID); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "has active premises") {
		t.Errorf("delete building with premises: err = %v", err)
	}
	if err := orgs.Delete(ctx, org.ID); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "has active buildings") {
		t.Errorf("delete org with buildings: err = %v", err)
	}
	acc, err := accounts.Create(ctx, model.Account{Number: "softdelete-1", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2020, 1, 1)})
	must(err)
	cleanup(t, pool, "personal_accounts", acc.ID)
	if err := premises.Delete(ctx, pr.ID); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "has active accounts") {
		t.Errorf("delete premises with account: err = %v", err)
	}

	// --- удаление, видимость, повторное удаление ---
	must(accounts.Delete(ctx, acc.ID))
	got, err := accounts.Get(ctx, acc.ID)
	if err != nil || got.DeletedAt == nil {
		t.Errorf("Get of deleted account: %+v, err = %v; want the record with deleted_at set", got, err)
	}
	active, err := accounts.List(ctx, model.AccountFilter{PremisesID: &pr.ID}, 50, 0)
	if err != nil || len(active) != 0 {
		t.Errorf("active list after delete: %d items, err = %v", len(active), err)
	}
	trash, err := accounts.List(ctx, model.AccountFilter{PremisesID: &pr.ID, Deleted: true}, 50, 0)
	if err != nil || len(trash) != 1 || trash[0].ID != acc.ID {
		t.Errorf("deleted list: %+v, err = %v", trash, err)
	}
	if err := accounts.Delete(ctx, acc.ID); !isKind(err, ErrNotFound) {
		t.Errorf("second delete: err = %v, want not found", err)
	}
	if _, err := accounts.Update(ctx, acc.ID, model.Account{Number: "x", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2020, 1, 1)}); !isKind(err, ErrNotFound) {
		t.Errorf("update of deleted account: err = %v, want not found", err)
	}
	nested, err := accounts.ListByPremises(ctx, pr.ID, true)
	if err != nil || len(nested) != 1 {
		t.Errorf("ListByPremises(deleted): %+v, err = %v", nested, err)
	}

	// --- повторное использование номера и конфликт при восстановлении ---
	acc2, err := accounts.Create(ctx, model.Account{Number: "softdelete-1", PremisesID: pr.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2021, 1, 1)})
	if err != nil {
		t.Fatalf("reuse of a deleted account number: %v", err)
	}
	cleanup(t, pool, "personal_accounts", acc2.ID)
	if _, err := accounts.Restore(ctx, acc.ID); !isKind(err, ErrConflict) {
		t.Errorf("restore with number taken: err = %v, want conflict", err)
	}
	must(accounts.Delete(ctx, acc2.ID))
	restored, err := accounts.Restore(ctx, acc.ID)
	if err != nil || restored.DeletedAt != nil {
		t.Errorf("restore: %+v, err = %v", restored, err)
	}
	if _, err := accounts.Restore(ctx, acc.ID); !isKind(err, ErrNotFound) {
		t.Errorf("restore of an active record: err = %v, want not found", err)
	}
	must(accounts.Delete(ctx, acc.ID))

	// --- нельзя ссылаться на удалённое и восстановить под удалённым родителем ---
	pr2, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "2"})
	must(err)
	cleanup(t, pool, "premises", pr2.ID)
	must(premises.Delete(ctx, pr2.ID))
	if _, err := accounts.Create(ctx, model.Account{Number: "softdelete-2", PremisesID: pr2.ID, Purpose: "utilities", Status: "active", OpenedAt: date(2020, 1, 1)}); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "premises is deleted") {
		t.Errorf("account under deleted premises: err = %v", err)
	}
	b2, err := buildings.Create(ctx, model.Building{OrganizationID: org.ID, Kind: "parking", Address: "softdelete-test-2"})
	must(err)
	cleanup(t, pool, "buildings", b2.ID)
	pr3, err := premises.Create(ctx, model.Premises{BuildingID: b2.ID, Kind: "parking_space", Number: "1"})
	must(err)
	cleanup(t, pool, "premises", pr3.ID)
	must(premises.Delete(ctx, pr3.ID))
	must(buildings.Delete(ctx, b2.ID))
	if _, err := premises.Restore(ctx, pr3.ID); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "building is deleted") {
		t.Errorf("restore under deleted building: err = %v", err)
	}
	if _, err := buildings.Restore(ctx, b2.ID); err != nil {
		t.Errorf("restore building: %v", err)
	}
	if _, err := premises.Restore(ctx, pr3.ID); err != nil {
		t.Errorf("restore premises after its building: %v", err)
	}

	// --- номер помещения освобождается (pr2 удалено выше) и конфликтует при восстановлении ---
	pr2b, err := premises.Create(ctx, model.Premises{BuildingID: b.ID, Kind: "apartment", Number: "2"})
	if err != nil {
		t.Fatalf("reuse of a deleted premises number: %v", err)
	}
	cleanup(t, pool, "premises", pr2b.ID)
	if _, err := premises.Restore(ctx, pr2.ID); !isKind(err, ErrConflict) {
		t.Errorf("restore premises with number taken: err = %v, want conflict", err)
	}

	// --- доли: удалённое владение не занимает долю, восстановление это проверяет ---
	newOwn := func(from model.Date) model.Ownership {
		return model.Ownership{PremisesID: pr.ID, PersonID: &person.ID, ShareNum: 1, ShareDen: 2, ValidFrom: from}
	}
	o1, err := owns.Create(ctx, newOwn(date(2020, 1, 1)))
	must(err)
	cleanup(t, pool, "ownerships", o1.ID)
	o2, err := owns.Create(ctx, newOwn(date(2020, 1, 1)))
	must(err)
	cleanup(t, pool, "ownerships", o2.ID)
	must(owns.Delete(ctx, o2.ID))
	o3, err := owns.Create(ctx, newOwn(date(2020, 1, 1)))
	if err != nil {
		t.Fatalf("share freed by a deleted ownership: %v", err)
	}
	cleanup(t, pool, "ownerships", o3.ID)
	if _, err := owns.Restore(ctx, o2.ID); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "exceeds 1") {
		t.Errorf("restore pushing shares over 1: err = %v", err)
	}
	if left, err := owns.ListByPremises(ctx, pr.ID, false); err != nil || len(left) != 2 {
		t.Errorf("active ownerships: %d, err = %v, want 2", len(left), err)
	}

	// --- физлицо: нельзя удалить, пока оно собственник; после удаления записи — можно ---
	if err := persons.Delete(ctx, person.ID); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "has active ownerships") {
		t.Errorf("delete owner: err = %v", err)
	}
	must(owns.Delete(ctx, o1.ID))
	must(owns.Delete(ctx, o3.ID))
	must(persons.Delete(ctx, person.ID))
	if found, err := persons.List(ctx, model.PersonFilter{Q: ptr("Удалённов")}, 50, 0); err != nil || len(found) != 0 {
		t.Errorf("deleted person must not be found by search: %+v, err = %v", found, err)
	}
	if _, err := owns.Restore(ctx, o1.ID); !isKind(err, ErrInvalid) || !strings.Contains(errText(err), "person is deleted") {
		t.Errorf("restore ownership of a deleted person: err = %v", err)
	}
}
