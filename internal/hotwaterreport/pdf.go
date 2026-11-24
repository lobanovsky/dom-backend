package hotwaterreport

import (
	"bytes"
	"fmt"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goregular"
)

func Render(period time.Time, apartments, offices float64) ([]byte, error) {
	months := [...]string{"январе", "феврале", "марте", "апреле", "мае", "июне", "июле", "августе", "сентябре", "октябре", "ноябре", "декабре"}
	pdf := fpdf.New("L", "mm", "A4", "")
	pdf.SetMargins(12, 10, 12)
	pdf.AddUTF8FontFromBytes("go", "", goregular.TTF)
	pdf.AddUTF8FontFromBytes("go", "B", gobold.TTF)
	pdf.AddPage()
	pdf.SetFont("go", "", 9)
	for _, line := range []string{
		"Приложение 3", "к Дополнительному соглашению", "от «______» _______________ 21_____ г.",
		"к Договору теплоснабжения", "от «______» _______________ 21_____ г.", "№ 03.209528ГВС",
	} {
		pdf.SetX(205)
		pdf.CellFormat(80, 4.5, line, "", 1, "L", false, 0, "")
	}
	pdf.Ln(3)
	pdf.SetFont("go", "B", 13)
	pdf.CellFormat(273, 7, "Справка об объемах горячей воды,", "", 1, "C", false, 0, "")
	pdf.SetFont("go", "", 10)
	for _, line := range []string{
		fmt.Sprintf("потребленной жилыми и нежилыми помещениями МКД в %s %d г. по адресу:", months[period.Month()-1], period.Year()),
		"г. Москва, 17-й проезд Марьиной Рощи, д.1", "ЦТП № 03.209528ГВС",
	} {
		pdf.CellFormat(273, 6, line, "", 1, "C", false, 0, "")
	}
	pdf.Ln(4)
	pdf.CellFormat(273, 5, "Договор", "1", 1, "L", false, 0, "")
	pdf.SetFont("go", "", 8)
	cell := func(x, y, width, height float64, lines ...string) {
		pdf.Rect(x, y, width, height, "D")
		for i, line := range lines {
			pdf.SetXY(x, y+(height-4*float64(len(lines)))/2+4*float64(i))
			pdf.CellFormat(width, 4, line, "", 0, "C", false, 0, "")
		}
	}
	widths := [...]float64{13, 35, 17, 17, 17, 24, 23, 30, 49, 48}
	y := pdf.GetY()
	cell(12, y, 13, 23, "№ п/п")
	cell(25, y, 35, 23, "Группа", "потребления")
	cell(60, y, 51, 9, "Количество зарегистрированных", "(проживающих) граждан")
	cell(111, y, 174, 9, "Количество потребленной горячей воды, куб. м")
	x := 60.0
	for i, lines := range [][]string{
		{"Всего"}, {"с ИПУ"}, {"без ИПУ"}, {"ВСЕГО"}, {"ИПУ"}, {"без ИПУ", "(норматив)"},
		{"Исходя из объемов", "среднемесячного", "потребления"}, {"Общедомовые", "нужды в МКД"},
	} {
		cell(x, y+9, widths[i+2], 14, lines...)
		x += widths[i+2]
	}
	y += 23
	format := func(value float64) string { return strings.ReplaceAll(fmt.Sprintf("%.3f", value), ".", ",") }
	a, o, total := format(apartments), format(offices), format(apartments+offices)
	for _, row := range [][]string{
		{"1", "2", "3=4+5", "4", "5", "6=7+8+9+10", "7", "8", "9", "10"},
		{"1", "Население", "143", "143", "0", a, a, "0", "0", "0"},
		{"2", "Прочие*", "0", "0", "0", o, o, "0", "0", "0"},
		{"3", "Бюджет*", "-", "-", "-", "-", "-", "-", "-", "-"},
		{"Итого", "", "143", "143", "0", total, total, "0", "0", "0"},
	} {
		x = 12
		for i, value := range row {
			width := widths[i]
			if row[0] == "Итого" {
				if i == 1 {
					continue
				}
				if i == 0 {
					width += widths[1]
				}
			}
			cell(x, y, width, 6, value)
			x += width
		}
		y += 6
	}
	pdf.SetXY(12, y+4)
	pdf.MultiCell(273, 4, "* Для владельцев нежилых помещений в МКД, относящихся к категории «Прочие», «Бюджет», объем потреблённой горячей воды (ст. №7) заполняется только по тем помещениям, которые оборудованы ИПУ.", "", "L", false)
	pdf.MultiCell(273, 4, "Примечание: Строка населения заполняется при отсутствии ОДУУ или выходе его из строя. В справке не указываются владельцы нежилых помещений МКД, имеющих прямой договор с Теплоснабжающей организацией.", "", "L", false)
	pdf.Ln(4)
	pdf.SetFont("go", "", 10)
	pdf.CellFormat(273, 7, "Председатель ТСН «МР17ДОМ1» _____________________________________ Шурыгин Д.М.", "", 1, "L", false, 0, "")
	pdf.CellFormat(273, 7, "Принял _____________________________________ ФИО (подпись)", "", 1, "L", false, 0, "")
	var out bytes.Buffer
	err := pdf.Output(&out)
	return out.Bytes(), err
}
