// Package personsreport рисует PDF отчёта «физлица и их недвижимость» (A4): человек, под ним его помещения с площадью и долей.
package personsreport

import (
	"bytes"
	"fmt"
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
		// Заголовок человека не оставляем одного внизу страницы: вместе с ним первая строка.
		ensure(7 + rowH(p.Items[0]))
		personTitle(n+1, p.Name, false)
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
