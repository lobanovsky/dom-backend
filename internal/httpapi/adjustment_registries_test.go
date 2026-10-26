package httpapi

import (
	"net/http"
	"testing"
)

// Разбор запроса корректировочного реестра отклоняет ошибки до обращения к хранилищу.
func TestAdjustmentRegistryRequestValidation(t *testing.T) {
	h := newTestRouter(t)
	c := login(t, h)
	for _, tc := range []struct {
		body string
		want int
	}{
		{`{}`, http.StatusUnprocessableEntity},
		{`{"bank_account_id":1,"personal_account_id":2,"amount":0,"payer_name":"x"}`, http.StatusUnprocessableEntity},
		{`{"bank_account_id":1,"personal_account_id":2,"amount":10.123,"payer_name":"x"}`, http.StatusUnprocessableEntity},
		{`{"bank_account_id":1,"personal_account_id":2,"amount":10,"payer_name":" "}`, http.StatusUnprocessableEntity},
		{`{"bank_account_id":1,"personal_account_id":2,"amount":10,"payer_name":"x","extra":1}`, http.StatusBadRequest},
	} {
		if rec := do(h, "POST", "/api/v1/adjustment-registries", tc.body, c); rec.Code != tc.want {
			t.Errorf("POST %s: status = %d, want %d", tc.body, rec.Code, tc.want)
		}
	}
	if rec := do(h, "GET", "/api/v1/adjustment-registries?resolved=maybe", "", c); rec.Code != http.StatusBadRequest {
		t.Errorf("bad filter: status = %d", rec.Code)
	}
}
