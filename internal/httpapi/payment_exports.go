package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"dom-backend/internal/model"
	"dom-backend/internal/store"
)

type PaymentExportStore interface {
	List(ctx context.Context, f model.PaymentExportFilter, limit, offset int) ([]model.PaymentExport, error)
	Get(ctx context.Context, id int64) (model.PaymentExport, error)
	File(ctx context.Context, id int64) (string, []byte, error)
	Preview(ctx context.Context, f model.IncomingPaymentFilter, sel store.ExportSelection) (store.ExportPreview, error)
	Create(ctx context.Context, f model.IncomingPaymentFilter, sel store.ExportSelection) (model.PaymentExport, error)
	Revoke(ctx context.Context, id int64) error
}

type paymentExportHandlers struct{ s PaymentExportStore }

func (h paymentExportHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/payment-exports", h.list)
	mux.HandleFunc("POST /api/v1/payment-exports/preview", h.preview)
	mux.HandleFunc("POST /api/v1/payment-exports", h.create)
	mux.HandleFunc("GET /api/v1/payment-exports/{id}", h.get)
	mux.HandleFunc("GET /api/v1/payment-exports/{id}/file", h.file)
	mux.HandleFunc("DELETE /api/v1/payment-exports/{id}", h.revoke)
}

func paymentExportFilter(w http.ResponseWriter, r *http.Request) (model.PaymentExportFilter, listParams, bool) {
	p, ok := parseListSpec(w, r, listSpec{Ints: []string{"bank_account_id"}})
	if !ok {
		return model.PaymentExportFilter{}, p, false
	}
	return model.PaymentExportFilter{Deleted: p.Deleted, BankAccountID: p.Int("bank_account_id")}, p, true
}

func (h paymentExportHandlers) list(w http.ResponseWriter, r *http.Request) {
	f, p, ok := paymentExportFilter(w, r)
	if !ok {
		return
	}
	items, err := h.s.List(r.Context(), f, p.Limit, p.Offset)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeList(w, items, p)
}

func (h paymentExportHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	item, err := h.s.Get(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// exportBody — выбор платежей. Фильтр передаётся параметрами запроса (те же, что у списка входящих платежей),
// а выбор — телом: mode=ids (ids — выбранные) или mode=filter (ids — исключённые).
type exportBody struct {
	Mode          string  `json:"mode"`
	IDs           []int64 `json:"ids"`
	ExpectedCount *int    `json:"expected_count"`
}

func readExportRequest(w http.ResponseWriter, r *http.Request) (model.IncomingPaymentFilter, store.ExportSelection, bool) {
	f, _, ok := incomingPaymentFilter(w, r)
	if !ok {
		return f, store.ExportSelection{}, false
	}
	var b exportBody
	if !decodeJSON(w, r, &b) {
		return f, store.ExportSelection{}, false
	}
	if b.Mode != "ids" && b.Mode != "filter" {
		writeError(w, http.StatusUnprocessableEntity, "mode: want ids or filter")
		return f, store.ExportSelection{}, false
	}
	if b.Mode == "ids" && len(b.IDs) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "ids: is required for mode ids")
		return f, store.ExportSelection{}, false
	}
	return f, store.ExportSelection{Mode: b.Mode, IDs: b.IDs, ExpectedCount: b.ExpectedCount}, true
}

// preview сообщает, сколько платежей и на какую сумму попадёт в реестр; ничего не меняет.
func (h paymentExportHandlers) preview(w http.ResponseWriter, r *http.Request) {
	f, sel, ok := readExportRequest(w, r)
	if !ok {
		return
	}
	res, err := h.s.Preview(r.Context(), f, sel)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (h paymentExportHandlers) create(w http.ResponseWriter, r *http.Request) {
	f, sel, ok := readExportRequest(w, r)
	if !ok {
		return
	}
	item, err := h.s.Create(r.Context(), f, sel)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (h paymentExportHandlers) file(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	name, data, err := h.s.File(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=windows-1251")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// revoke отзывает выгрузку: платежи снова считаются невыгруженными.
func (h paymentExportHandlers) revoke(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := h.s.Revoke(r.Context(), id); err != nil {
		writeErr(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
