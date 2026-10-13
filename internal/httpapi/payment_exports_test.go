package httpapi

import (
	"net/http"
	"testing"
)

// Разбор запроса выгрузки отклоняет ошибки до обращения к хранилищу.
func TestPaymentExportRequestValidation(t *testing.T) {
	h := newTestRouter(t)
	c := login(t, h)
	for _, tc := range []struct {
		path, body string
		want       int
	}{
		{"/api/v1/payment-exports/preview", `{}`, http.StatusUnprocessableEntity},
		{"/api/v1/payment-exports/preview", `{"mode":"all"}`, http.StatusUnprocessableEntity},
		{"/api/v1/payment-exports", `{"mode":"ids","ids":[]}`, http.StatusUnprocessableEntity},
		{"/api/v1/payment-exports", `{"mode":"filter","extra":1}`, http.StatusBadRequest},
		{"/api/v1/payment-exports?export=other", `{"mode":"filter"}`, http.StatusBadRequest},
		{"/api/v1/payment-exports?unknown=1", `{"mode":"filter"}`, http.StatusBadRequest},
	} {
		if rec := do(h, "POST", tc.path, tc.body, c); rec.Code != tc.want {
			t.Errorf("POST %s %s: status = %d, want %d", tc.path, tc.body, rec.Code, tc.want)
		}
	}
}
