package httpapi

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"dom-backend/internal/model"
	"dom-backend/internal/sberregistry"
)

const maxRegistrySize = 5 << 20

type PaymentRegistryStore interface {
	List(ctx context.Context, f model.PaymentRegistryFilter, limit, offset int) ([]model.PaymentRegistry, error)
	Get(ctx context.Context, id int64) (model.PaymentRegistry, error)
	File(ctx context.Context, id int64) (string, []byte, error)
	Import(ctx context.Context, bankAccountID int64, fileName string, data []byte, reg *model.ParsedRegistry) (model.RegistryImportResult, error)
}

type paymentRegistryHandlers struct{ s PaymentRegistryStore }

func (h paymentRegistryHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/payment-registries", h.list)
	mux.HandleFunc("POST /api/v1/payment-registries/import", h.importFile)
	mux.HandleFunc("GET /api/v1/payment-registries/{id}", h.get)
	mux.HandleFunc("GET /api/v1/payment-registries/{id}/file", h.file)
}

func (h paymentRegistryHandlers) list(w http.ResponseWriter, r *http.Request) {
	f, p, ok := paymentRegistryFilter(w, r)
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

func (h paymentRegistryHandlers) get(w http.ResponseWriter, r *http.Request) {
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

// file отдаёт исходный загруженный файл реестра.
func (h paymentRegistryHandlers) file(w http.ResponseWriter, r *http.Request) {
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

// importFile принимает multipart/form-data: bank_account_id (счёт-получатель) и файл реестра в поле file.
func (h paymentRegistryHandlers) importFile(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRegistrySize+1<<20)
	if err := r.ParseMultipartForm(maxRegistrySize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	bankAccountID, err := strconv.ParseInt(r.FormValue("bank_account_id"), 10, 64)
	if err != nil || bankAccountID < 1 {
		writeError(w, http.StatusUnprocessableEntity, "bank_account_id: is required")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `field "file" with a registry file is required`)
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxRegistrySize+1))
	if err != nil || len(data) > maxRegistrySize {
		writeError(w, http.StatusRequestEntityTooLarge, "file is too large")
		return
	}

	reg, rowErrs, err := sberregistry.Parse(header.Filename, data)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(rowErrs) > 0 {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "file contains invalid rows, nothing was imported",
			"rows":  rowErrs,
		})
		return
	}
	res, err := h.s.Import(r.Context(), bankAccountID, header.Filename, data, reg)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}
