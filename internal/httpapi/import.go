package httpapi

import (
	"context"
	"net/http"
	"slices"
	"strings"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsximport"
)

const maxImportSize = 10 << 20

type ImportStore interface {
	Import(ctx context.Context, buildingID int64, kind string, rows []model.ImportRow) (model.ImportResult, error)
}

type importHandlers struct{ store ImportStore }

func (h importHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/buildings/{id}/import", h.importFile)
}

// importFile принимает multipart/form-data: поле kind (вид помещений) и xlsx-файл в поле file.
func (h importHandlers) importFile(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)
	if err := r.ParseMultipartForm(maxImportSize); err != nil {
		writeError(w, http.StatusBadRequest, "invalid multipart body: "+err.Error())
		return
	}
	kind := r.FormValue("kind")
	if !slices.Contains(model.PremisesKinds, kind) {
		writeError(w, http.StatusUnprocessableEntity, "kind: must be one of: "+strings.Join(model.PremisesKinds, ", "))
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, `field "file" with an xlsx file is required`)
		return
	}
	defer file.Close()

	rows, rowErrs, err := xlsximport.Parse(file)
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
	res, err := h.store.Import(r.Context(), id, kind, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, res)
}
