package httpapi

import (
	"context"
	"net/http"
	"strings"
	"time"

	"dom-backend/internal/calc"
	"dom-backend/internal/model"
)

// CalculationStore считает стоимость услуг лицевого счёта за месяц (ничего не записывает).
type CalculationStore interface {
	AccountCalculation(ctx context.Context, accountID int64, period model.Date) (calc.Result, error)
}

type calculationHandlers struct{ s CalculationStore }

func (h calculationHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/accounts/{id}/calculation", h.account)
}

// account: GET /api/v1/accounts/{id}/calculation?period=YYYY-MM — расчёт за месяц; период обязателен.
func (h calculationHandlers) account(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	v := strings.TrimSpace(r.URL.Query().Get("period"))
	if v == "" {
		writeError(w, http.StatusUnprocessableEntity, "period: is required")
		return
	}
	if len(v) == 7 { // YYYY-MM из поля выбора месяца
		v += "-01"
	}
	t, err := time.Parse("2006-01-02", v)
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, "period: month must be YYYY-MM")
		return
	}
	res, err := h.s.AccountCalculation(r.Context(), id, model.Date{Time: t})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
