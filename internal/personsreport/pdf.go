// Package personsreport рисует PDF отчёта «физлица и их недвижимость» (A4): человек, под ним его помещения с площадью и долей.
package personsreport

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"dom-backend/internal/model"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

const (
	marginX    = 10.0
	lineHeight = 4.6
	fontSize   = 9.0
	fontFamily = "go"
	pageLimit  = 297 - 14.0
)

// Ширины колонок: помещение, площадь, доля, кадастровый номер (в сумме 190 мм).
var colWidths = [4]float64{62, 28, 28, 72}
var colTitles = [4]string{"Помещение", "Площадь, м²", "Доля владения", "Кадастровый номер"}

func Render(rep model.PersonsReport) ([]byte, error) {
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginX, 12, marginX)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AddUTF8FontFromBytes(fontFamily, "", goregular.TTF)
	pdf.AddUTF8FontFromBytes(fontFamily, "B", gobold.TTF)
	pdf.SetTitle("Физические лица и их недвижимость", true)
	pdf.AliasNbPages("{nb}")
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont(fontFamily, "", 8)
		pdf.CellFormat(0, 5, fmt.Sprintf("Страница %d из {nb}", pdf.PageNo()), "", 0, "C", false, 0, "")
	})

	tableHeader := func() {
		pdf.SetFont(fontFamily, "B", fontSize)
		pdf.SetFillColor(235, 238, 242)
		for i, t := range colTitles {
			pdf.CellFormat(colWidths[i], 7, t, "1", 0, "C", true, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetFont(fontFamily, "", fontSize)
	}
	pdf.AddPage()
	pdf.SetFont(fontFamily, "B", 14)
	pdf.CellFormat(0, 8, "Физические лица и их недвижимость", "", 1, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)
	pdf.CellFormat(0, 5.6, fmt.Sprintf("Состояние на %s. Физлиц: %d.", formatDay(rep.Day), len(rep.Persons)), "", 1, "L", false, 0, "")
	pdf.Ln(3)
	tableHeader()

	// ensure переносит на новую страницу, если следующий блок высотой h не помещается.
	ensure := func(h float64) {
		if pdf.GetY()+h > pageLimit {
			pdf.AddPage()
			tableHeader()
		}
	}
	personTitle := func(n int, name string, cont bool) {
		pdf.SetFont(fontFamily, "B", 10)
		pdf.SetFillColor(246, 247, 249)
		title := fmt.Sprintf("%d. %s", n, name)
		if cont {
			title += " (продолжение)"
		}
		pdf.CellFormat(190, 7, title, "1", 1, "L", true, 0, "")
		pdf.SetFont(fontFamily, "", fontSize)
	}

	for n, p := range rep.Persons {
		rowH := func(it model.PersonsReportItem) float64 {
			lines := 1
			if rep.MultipleBuildings && it.Address != "" {
				lines = 2
			}
			return float64(lines)*lineHeight + 1.6
		}
		// Заголовок человека не оставляем одного внизу страницы: вместе с ним контакты и первая строка.
		contacts := contactLines(pdf, p)
		contactsH := float64(len(contacts))*lineHeight + 1.6
		ensure(7 + contactsH + rowH(p.Items[0]))
		personTitle(n+1, p.Name, false)
		personContacts(pdf, contacts, contactsH)
		for _, it := range p.Items {
			h := rowH(it)
			if pdf.GetY()+h > pageLimit {
				pdf.AddPage()
				tableHeader()
				personTitle(n+1, p.Name, true)
			}
			x, y := pdf.GetXY()
			area := "—"
			if it.Area != nil {
				area = formatNumber(*it.Area)
			}
			cells := [4]string{it.Label, area, fmt.Sprintf("%d/%d", it.ShareNum, it.ShareDen), it.Cadastral}
			aligns := [4]string{"L", "R", "R", "L"}
			for i, c := range cells {
				pdf.Rect(x, y, colWidths[i], h, "D")
				pdf.SetXY(x, y+0.8)
				pdf.CellFormat(colWidths[i], lineHeight, c, "", 0, aligns[i], false, 0, "")
				if i == 0 && rep.MultipleBuildings && it.Address != "" {
					pdf.SetXY(x, y+0.8+lineHeight)
					pdf.SetFont(fontFamily, "", 7.5)
					pdf.CellFormat(colWidths[i], lineHeight, it.Address, "", 0, "L", false, 0, "")
					pdf.SetFont(fontFamily, "", fontSize)
				}
				x += colWidths[i]
			}
			pdf.SetXY(marginX, y+h)
		}
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

var (
	nonPhoneChars = regexp.MustCompile(`[^\d\s()+\-.]`)
	nonDigits     = regexp.MustCompile(`\D`)
)

// formatPhone приводит российский номер к виду «+7 (926) 793-63-63» (из «+79267936363», «89267936363», «9267936363»);
// иностранные номера, записанные только цифрами, показываются с «+»; «доб.» и короткие остаются как есть. В базе номер хранится только цифрами.
func formatPhone(raw string) string {
	raw = strings.TrimSpace(raw)
	if nonPhoneChars.MatchString(raw) {
		return raw
	}
	digits := nonDigits.ReplaceAllString(raw, "")
	plus := strings.HasPrefix(raw, "+")
	var d string
	switch {
	case len(digits) == 11 && (digits[0] == '7' || (digits[0] == '8' && !plus)):
		d = "7" + digits[1:]
	case len(digits) == 10 && !plus:
		d = "7" + digits
	case !plus && len(digits) == len(raw) && len(digits) >= 11 && len(digits) <= 15:
		return "+" + digits // хранится только цифрами: иностранный номер показываем с «+»
	default:
		return raw
	}
	return fmt.Sprintf("+7 (%s) %s-%s-%s", d[1:4], d[4:7], d[7:9], d[9:11])
}

// contactLines — строки «Телефон: … Email: …» с переносом по ширине таблицы; пустые значения — прочерк.
func contactLines(pdf *fpdf.Fpdf, p model.PersonsReportPerson) []string {
	join := func(items []string) string {
		if len(items) == 0 {
			return "—"
		}
		return strings.Join(items, ", ")
	}
	pdf.SetFont(fontFamily, "", fontSize)
	return pdf.SplitText("Телефон: "+join(formatPhones(p.Phones))+"     Email: "+join(p.Emails), 186)
}

func formatPhones(items []string) []string {
	out := make([]string, len(items))
	for i, s := range items {
		out[i] = formatPhone(s)
	}
	return out
}

func personContacts(pdf *fpdf.Fpdf, lines []string, h float64) {
	x, y := pdf.GetXY()
	pdf.Rect(x, y, 190, h, "D")
	pdf.SetXY(x+1, y+0.8)
	for _, l := range lines {
		pdf.CellFormat(188, lineHeight, l, "", 2, "L", false, 0, "")
		pdf.SetX(x + 1)
	}
	pdf.SetXY(x, y+h)
}

// formatNumber: 97.2 -> «97,2», 97 -> «97»; не больше двух знаков.
func formatNumber(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return strings.Replace(s, ".", ",", 1)
}

func formatDay(day string) string {
	if len(day) == 10 {
		return day[8:10] + "." + day[5:7] + "." + day[0:4]
	}
	return day
}
