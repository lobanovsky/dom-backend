package httpapi

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"dom-backend/internal/model"
	"dom-backend/internal/xlsxexport"
)

// maxExcelRows — предел строк в выгрузке в Excel: файл остаётся разумного размера, а запрос быстрым.
const maxExcelRows = 20000

// paymentExcelHandlers — выгрузка списков платежей в Excel по тем же фильтрам, что и сами списки.
type paymentExcelHandlers struct {
	incoming   crudStore[model.IncomingPayment, model.IncomingPaymentFilter]
	outgoing   crudStore[model.OutgoingPayment, model.OutgoingPaymentFilter]
	banks      crudStore[model.BankAccount, model.BankAccountFilter]
	categories crudStore[model.PaymentCategory, model.PaymentCategoryFilter]
}

func (h paymentExcelHandlers) register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/v1/incoming-payments/export", h.exportIncoming)
	mux.HandleFunc("GET /api/v1/outgoing-payments/export", h.exportOutgoing)
}

// names возвращает подписи банковских счетов (номер) и категорий по id.
func (h paymentExcelHandlers) names(r *http.Request) (banks, cats map[int64]string, err error) {
	bl, err := h.banks.List(r.Context(), model.BankAccountFilter{}, 1000, 0)
	if err != nil {
		return nil, nil, err
	}
	cl, err := h.categories.List(r.Context(), model.PaymentCategoryFilter{}, 1000, 0)
	if err != nil {
		return nil, nil, err
	}
	banks, cats = map[int64]string{}, map[int64]string{}
	for _, b := range bl {
		banks[b.ID] = b.Number
	}
	for _, c := range cl {
		cats[c.ID] = c.Name
	}
	return banks, cats, nil
}

func (h paymentExcelHandlers) exportIncoming(w http.ResponseWriter, r *http.Request) {
	f, _, ok := incomingPaymentFilter(w, r)
	if !ok {
		return
	}
	f.Deleted = false
	items, err := h.incoming.List(r.Context(), f, maxExcelRows+1, 0)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(items) > maxExcelRows {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("too many payments: at most %d for export", maxExcelRows))
		return
	}
	banks, cats, err := h.names(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	cols := []xlsxexport.Column{
		{Title: "Дата", Width: 12, Kind: xlsxexport.Date}, {Title: "Время", Width: 10}, {Title: "Банковский счёт", Width: 24},
		{Title: "От кого", Width: 32}, {Title: "ИНН плательщика", Width: 16}, {Title: "Сумма", Width: 14, Kind: xlsxexport.Money},
		{Title: "Лицевой счёт", Width: 14}, {Title: "Категория", Width: 22}, {Title: "Назначение", Width: 60}, {Title: "Комментарий", Width: 40},
		{Title: "Обратить внимание", Width: 12}, {Title: "Источник", Width: 30}, {Title: "Выгружен в реестр", Width: 36},
	}
	rows := make([][]any, 0, len(items))
	for _, p := range items {
		tm := ""
		if p.PaymentTime != nil {
			tm = string(*p.PaymentTime)
		}
		attention := ""
		if p.Attention {
			attention = "Да"
		}
		rows = append(rows, []any{
			xlsxexport.Day(p.PaymentDate.Time), tm, banks[p.BankAccountID], p.PayerName, str(p.PayerINN), p.Amount,
			str(p.PersonalAccountNumber), catName(cats, p.CategoryID), str(p.Purpose), str(p.Comment),
			attention, incomingSource(p), str(p.ExportFileName),
		})
	}
	writeXLSX(w, "incoming-payments", "Входящие платежи", cols, rows)
}

func (h paymentExcelHandlers) exportOutgoing(w http.ResponseWriter, r *http.Request) {
	f, _, ok := outgoingPaymentFilter(w, r)
	if !ok {
		return
	}
	f.Deleted = false
	items, err := h.outgoing.List(r.Context(), f, maxExcelRows+1, 0)
	if err != nil {
		writeErr(w, err)
		return
	}
	if len(items) > maxExcelRows {
		writeError(w, http.StatusUnprocessableEntity, fmt.Sprintf("too many payments: at most %d for export", maxExcelRows))
		return
	}
	banks, cats, err := h.names(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	cols := []xlsxexport.Column{
		{Title: "Дата", Width: 12, Kind: xlsxexport.Date}, {Title: "Банковский счёт", Width: 24}, {Title: "Кому", Width: 32},
		{Title: "ИНН получателя", Width: 16}, {Title: "Сумма", Width: 14, Kind: xlsxexport.Money}, {Title: "Категория", Width: 22},
		{Title: "Назначение", Width: 60}, {Title: "Комментарий", Width: 40},
	}
	rows := make([][]any, 0, len(items))
	for _, p := range items {
		rows = append(rows, []any{
			xlsxexport.Day(p.PaymentDate.Time), banks[p.BankAccountID], p.RecipientName, str(p.RecipientINN), p.Amount,
			catName(cats, p.CategoryID), str(p.Purpose), str(p.Comment),
		})
	}
	writeXLSX(w, "outgoing-payments", "Исходящие платежи", cols, rows)
}

func incomingSource(p model.IncomingPayment) string {
	switch {
	case p.RegistryFileName != nil:
		return "Реестр " + *p.RegistryFileName
	case p.StatementID != nil:
		return "Выписка № " + strconv.FormatInt(*p.StatementID, 10)
	}
	return "Вручную"
}

func catName(cats map[int64]string, id *int64) string {
	if id == nil {
		return ""
	}
	return cats[*id]
}

func str(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func writeXLSX(w http.ResponseWriter, prefix, sheet string, cols []xlsxexport.Column, rows [][]any) {
	data, err := xlsxexport.Build(sheet, cols, rows)
	if err != nil {
		writeErr(w, err)
		return
	}
	name := fmt.Sprintf("%s-%s.xlsx", prefix, time.Now().Format("2006-01-02"))
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
