package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xuri/excelize/v2"

	"dom-backend/internal/model"
)

type fakeIncoming struct {
	crudStore[model.IncomingPayment, model.IncomingPaymentFilter]
	items []model.IncomingPayment
}

func (f fakeIncoming) List(context.Context, model.IncomingPaymentFilter, int, int) ([]model.IncomingPayment, error) {
	return f.items, nil
}

type fakeOutgoing struct {
	crudStore[model.OutgoingPayment, model.OutgoingPaymentFilter]
}

func (fakeOutgoing) List(context.Context, model.OutgoingPaymentFilter, int, int) ([]model.OutgoingPayment, error) {
	return []model.OutgoingPayment{{BankAccountID: 1, PaymentDate: model.NewDate(2026, 8, 3), Amount: 99.5, RecipientName: "ООО Ромашка", CategoryID: ptr(int64(5))}}, nil
}

type fakeBanks struct {
	crudStore[model.BankAccount, model.BankAccountFilter]
}

func (fakeBanks) List(context.Context, model.BankAccountFilter, int, int) ([]model.BankAccount, error) {
	return []model.BankAccount{{Meta: model.Meta{ID: 1}, Number: "40703810000000009981"}}, nil
}

type fakeCategories struct {
	crudStore[model.PaymentCategory, model.PaymentCategoryFilter]
}

func (fakeCategories) List(context.Context, model.PaymentCategoryFilter, int, int) ([]model.PaymentCategory, error) {
	return []model.PaymentCategory{{Meta: model.Meta{ID: 5}, Name: "Аренда"}}, nil
}

func excelRequest(h paymentExcelHandlers, path string) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	h.register(mux)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	return rec
}

func TestPaymentsExcel(t *testing.T) {
	clock := model.Clock("09:32:33")
	h := paymentExcelHandlers{
		incoming: fakeIncoming{items: []model.IncomingPayment{{
			BankAccountID: 1, PaymentDate: model.NewDate(2026, 8, 3), PaymentTime: &clock, Amount: 1234.5, PayerName: "ИВАНОВ",
			PersonalAccountNumber: ptr("0009991001"), Attention: true, StatementID: ptr(int64(7)), Comment: ptr("проверить"),
		}}},
		outgoing: fakeOutgoing{}, banks: fakeBanks{}, categories: fakeCategories{},
	}

	rec := excelRequest(h, "/api/v1/incoming-payments/export?bank_account_id=1")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("incoming: status = %d, type = %q", rec.Code, rec.Header().Get("Content-Type"))
	}
	f, err := excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	rows, _ := f.GetRows("Входящие платежи")
	if len(rows) != 2 || rows[1][2] != "40703810000000009981" || rows[1][3] != "ИВАНОВ" || rows[1][6] != "0009991001" || rows[1][10] != "Да" || rows[1][11] != "Выписка № 7" {
		t.Errorf("incoming rows = %v", rows)
	}

	rec = excelRequest(h, "/api/v1/outgoing-payments/export")
	f, err = excelize.OpenReader(bytes.NewReader(rec.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	rows, _ = f.GetRows("Исходящие платежи")
	if len(rows) != 2 || rows[1][2] != "ООО Ромашка" || rows[1][5] != "Аренда" {
		t.Errorf("outgoing rows = %v", rows)
	}

	if rec := excelRequest(h, "/api/v1/incoming-payments/export?export=other"); rec.Code != http.StatusBadRequest {
		t.Errorf("bad filter: status = %d, want 400", rec.Code)
	}
	h.incoming = fakeIncoming{items: make([]model.IncomingPayment, maxExcelRows+1)}
	if rec := excelRequest(h, "/api/v1/incoming-payments/export"); rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("too many: status = %d, want 422", rec.Code)
	}
}
