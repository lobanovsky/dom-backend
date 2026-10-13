// Package sberexport формирует файл реестра платежей в Сбер-формате — того же, который разбирает sberregistry.
// Файл предназначен для загрузки во внешнюю систему.
package sberexport

import (
	"fmt"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"
)

// Константные поля строки реестра (значения согласованы с принимающей системой).
const (
	field3  = "1111"
	field4  = "1111999V"
	field13 = "1"
)

// Row — один платёж реестра.
type Row struct {
	ID            int64     // идентификатор платежа: из него строится номер операции
	Date          time.Time // дата платежа
	Time          string    // чч:мм:сс или пусто (в выписках времени нет)
	Amount        int64     // копейки
	PayerName     string
	AccountNumber string // номер лицевого счёта
	Address       string
}

// Registry — выгрузка для одного банковского счёта.
type Registry struct {
	OrgINN      string
	BankAccount string // 20 цифр
	Number      int    // порядковый номер реестра в пределах счёта
	Date        time.Time
	Rows        []Row
}

// FileName — имя файла: <ИНН>_<счёт>_<номер>.txt (разбирается sberregistry.FileAccount).
func (r Registry) FileName() string {
	return fmt.Sprintf("%s_%s_%d.txt", r.OrgINN, r.BankAccount, r.Number)
}

// OperationNumber — номер операции в реестре: идентификатор платежа, дополненный нулями до 12 знаков.
func OperationNumber(id int64) string { return fmt.Sprintf("%012d", id) }

// Build возвращает содержимое файла в кодировке cp1251.
func (r Registry) Build() ([]byte, error) {
	var b strings.Builder
	var total int64
	for _, p := range r.Rows {
		tm := "00-00-00"
		if p.Time != "" {
			tm = strings.ReplaceAll(p.Time, ":", "-")
		}
		sum := money(p.Amount)
		fmt.Fprintf(&b, "%s;%s;%s;%s;%s;%s;%s;%s;%s;%s;%s;0,00;%s\r\n",
			p.Date.Format("02-01-2006"), tm, field3, field4, OperationNumber(p.ID), clean(p.AccountNumber),
			clean(p.PayerName), clean(p.Address), p.Date.Format("0106"), sum, sum, field13)
		total += p.Amount
	}
	fmt.Fprintf(&b, "=%d;%s;%s;0,00;%d;%s\r\n", len(r.Rows), money(total), money(total), r.Number, r.Date.Format("02-01-2006"))
	out, err := charmap.Windows1251.NewEncoder().Bytes([]byte(b.String()))
	if err != nil {
		return nil, fmt.Errorf("cannot encode registry as cp1251: %w", err)
	}
	return out, nil
}

// money форматирует копейки как «123,45».
func money(k int64) string { return fmt.Sprintf("%d,%02d", k/100, k%100) }

// clean убирает из текста символы, ломающие разметку файла.
func clean(s string) string {
	s = strings.Map(func(r rune) rune {
		if r == ';' || r == '\r' || r == '\n' {
			return ' '
		}
		return r
	}, s)
	return strings.Join(strings.Fields(s), " ")
}
