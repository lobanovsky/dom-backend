// Package ownersregistry рисует PDF реестра собственников дома (A4, таблица с шапкой на каждой странице).
package ownersregistry

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
	fontFamily = "go" // Go fonts покрывают кириллицу и вшиты в бинарник
)

// Ширины колонок: номер, площадь, собственники, доля, кадастровый номер (в сумме 190 мм).
var colWidths = [5]float64{28, 20, 74, 24, 44}
var colTitles = [5]string{"Номер помещения", "Площадь, м²", "ФИО собственника", "Доля в праве ОИ, %", "Кадастровый номер"}

// Render возвращает PDF реестра. Доля в праве на общее имущество = площадь помещения / общая площадь помещений дома.
func Render(reg model.OwnersRegistry) ([]byte, error) {
	total := reg.TotalArea()
	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(marginX, 12, marginX)
	pdf.SetAutoPageBreak(true, 14)
	pdf.AddUTF8FontFromBytes(fontFamily, "", goregular.TTF)
	pdf.AddUTF8FontFromBytes(fontFamily, "B", gobold.TTF)
	pdf.SetTitle("Реестр собственников: "+reg.Address, true)
	pdf.AliasNbPages("{nb}")

	tableHeader := func() {
		pdf.SetFont(fontFamily, "B", fontSize)
		pdf.SetFillColor(235, 238, 242)
		const h = 2*lineHeight + 1.6 // названия колонок переносятся на вторую строку
		x, y := pdf.GetXY()
		for i, t := range colTitles {
			pdf.Rect(x, y, colWidths[i], h, "DF")
			lines := pdf.SplitText(t, colWidths[i]-2)
			pdf.SetXY(x, y+(h-float64(len(lines))*lineHeight)/2)
			for _, l := range lines {
				pdf.CellFormat(colWidths[i], lineHeight, l, "", 2, "C", false, 0, "")
				pdf.SetX(x)
			}
			x += colWidths[i]
		}
		pdf.SetXY(marginX, y+h)
		pdf.SetFont(fontFamily, "", fontSize)
	}
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont(fontFamily, "", 8)
		pdf.CellFormat(0, 5, fmt.Sprintf("Страница %d из {nb}", pdf.PageNo()), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()

	pdf.SetFont(fontFamily, "B", 14)
	pdf.CellFormat(0, 8, "Реестр собственников помещений", "", 1, "L", false, 0, "")
	pdf.SetFont(fontFamily, "", 10)
	for _, line := range [][2]string{
		{"Адрес дома", reg.Address},
		{"Общая площадь помещений", formatNumber(total) + " кв.м."},
		{"Общая доля собственности", "100 %"},
		{"Состояние на", formatDay(reg.Day)},
	} {
		pdf.SetFont(fontFamily, "B", 10)
		pdf.CellFormat(58, 5.6, line[0], "", 0, "L", false, 0, "")
		pdf.SetFont(fontFamily, "", 10)
		pdf.MultiCell(0, 5.6, line[1], "", "L", false)
	}
	pdf.Ln(3)
	tableHeader()

	for _, row := range reg.Rows {
		owners := strings.Join(row.Owners, ", ")
		if owners == "" {
			owners = "—"
		}
		share := "—"
		if row.Area != nil && total > 0 {
			share = formatShare(*row.Area / total * 100)
		}
		area := "—"
		if row.Area != nil {
			area = formatNumber(*row.Area)
		}
		cells := [5]string{row.Label, area, owners, share, row.Cadastral}
		aligns := [5]string{"L", "R", "L", "R", "L"}

		// Высота строки — по самой длинной ячейке; перенос на новую страницу целой строкой.
		lines := 1
		wrapped := [5][]string{}
		for i, c := range cells {
			wrapped[i] = pdf.SplitText(c, colWidths[i]-2)
			if len(wrapped[i]) > lines {
				lines = len(wrapped[i])
			}
		}
		h := float64(lines)*lineHeight + 1.6
		if pdf.GetY()+h > 297-14 {
			pdf.AddPage()
			tableHeader()
		}
		x, y := pdf.GetXY()
		for i := range cells {
			pdf.Rect(x, y, colWidths[i], h, "D")
			pdf.SetXY(x, y+0.8)
			for _, l := range wrapped[i] {
				pdf.CellFormat(colWidths[i], lineHeight, l, "", 2, aligns[i], false, 0, "")
				pdf.SetX(x)
			}
			x += colWidths[i]
		}
		pdf.SetXY(marginX, y+h)
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

// formatShare: процент с двумя знаками, запятая как разделитель («0,88»).
func formatShare(v float64) string {
	return strings.Replace(strconv.FormatFloat(v, 'f', 2, 64), ".", ",", 1)
}

func formatDay(day string) string {
	if len(day) == 10 {
		return day[8:10] + "." + day[5:7] + "." + day[0:4]
	}
	return day
}
