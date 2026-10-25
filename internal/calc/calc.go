// Package calc считает стоимость услуг лицевого счёта за месяц. Пакет не ходит в БД: все данные (назначенные услуги,
// цены, нормативы, счётчики и показания) приходят в Input, расчёт ведётся точной арифметикой (big.Rat), округление
// только в конце: количество — до 4 знаков, сумма строки — до копеек (half up), итог — сумма округлённых строк,
// чтобы квитанция сходилась построчно.
package calc

import (
	"math/big"
	"sort"
	"time"
)

// Статусы строки расчёта. Всё, кроме StatusOK, означает, что сумму посчитать нельзя: строка не входит в итог.
const (
	StatusOK              = "ok"
	StatusNoPrice         = "no_price"         // на начало месяца нет цены (тарифа)
	StatusNoArea          = "no_area"          // у помещения не указана площадь (или нечего делить)
	StatusNoNorm          = "no_norm"          // у дома нет норматива на месяц
	StatusNoMeters        = "no_meters"        // нет подходящих счётчиков
	StatusNoReading       = "no_reading"       // показания за месяц не переданы
	StatusNoPrevious      = "no_previous"      // нет предыдущего показания: первое показание счётчика — только точка отсчёта
	StatusNegative        = "negative"         // показание меньше предыдущего
	StatusNoQuantity      = "no_quantity"      // не задано количество (ручная услуга)
	StatusComponentFailed = "component_failed" // не посчиталась одна из услуг состава
	// StatusRetired — только у счётчика в строке расчёта: снят с учёта в этом месяце и показаний за месяц нет; расчёт не блокирует.
	StatusRetired = "retired"
)

// Предупреждения: расчёт выполнен, но на это стоит посмотреть.
const (
	WarnPriceChanges = "price_changes" // цена меняется внутри месяца: взята цена на первое число
	WarnNormChanges  = "norm_changes"  // норматив меняется внутри месяца: взят норматив на первое число
	WarnAreaMissing  = "area_missing"  // у части помещений, между которыми делится расход, нет площади
)

// Reading — показание счётчика по тарифам (T[0] — Т1).
type Reading struct {
	Period time.Time
	T      [3]*big.Rat
}

// Meter — счётчик с показанием расчётного месяца (Current) и предыдущим показанием (Previous, период раньше расчётного).
type Meter struct {
	ID          int64
	Serial      string
	Name        string // название счётчика («ИТП»); может быть пустым
	Resource    string
	Tariffs     int
	Coefficient *big.Rat
	InstalledAt time.Time
	RemovedAt   *time.Time
	Initial     [3]*big.Rat
	Current     *Reading
	Previous    *Reading
}

// Service — услуга, участвующая в расчёте (назначенная счёту или входящая в состав другой).
type Service struct {
	ID            int64
	Name          string
	ReceiptName   string
	Kind          string
	Unit          string
	Method        string
	Position      int
	MeterResource string
	MeterTariff   int
	PriceFirst    *big.Rat // цена единицы на первое число месяца (nil — нет цены; у sum_prices всегда nil)
	PriceLast     *big.Rat // то же на последнее число месяца
	NormFirst     *big.Rat // норматив дома на первое число месяца (per_area_norm)
	NormLast      *big.Rat
	ComponentIDs  []int64
	CommonMeters  []Meter  // общедомовые счётчики услуги в доме помещения (common_meter)
	ShareBase     *big.Rat // сумма площадей помещений, между которыми делится расход общедомовых счётчиков
	ShareNoArea   int      // сколько из этих помещений без площади
}

// Assignment — назначение услуги счёту, действующее в месяце; Quantity задано у ручных услуг.
type Assignment struct {
	ServiceID int64
	Quantity  *big.Rat
}

type Input struct {
	Period        time.Time // первое число месяца
	AccountID     int64
	AccountNumber string
	Area          *big.Rat // площадь помещения счёта (nil — не указана)
	Meters        []Meter  // индивидуальные счётчики помещения
	Assignments   []Assignment
	Services      map[int64]*Service
}

// MeterUse — расход одного счётчика за месяц.
type MeterUse struct {
	MeterID     int64    `json:"meter_id"`
	Serial      string   `json:"serial_number"`
	Name        string   `json:"name"`
	Previous    *float64 `json:"previous"`
	Current     *float64 `json:"current"`
	Consumption *float64 `json:"consumption"`
	Status      string   `json:"status"`
}

// Line — строка расчёта: одна услуга. Quantity/Price/Amount пусты, если посчитать нельзя (Status != ok).
type Line struct {
	ServiceID   int64      `json:"service_id"`
	Name        string     `json:"name"` // название в квитанции
	Kind        string     `json:"kind"`
	Unit        string     `json:"unit"`
	CalcMethod  string     `json:"calc_method"`
	Quantity    *float64   `json:"quantity"`
	Price       *float64   `json:"price"`
	Amount      *float64   `json:"amount"`
	Status      string     `json:"status"`
	Failed      string     `json:"failed_component,omitempty"` // название слагаемого, которое не посчиталось
	Warnings    []string   `json:"warnings"`
	Area        *float64   `json:"area"`         // площадь помещения (по площади, с нормативом, общедомовой)
	Norm        *float64   `json:"norm"`         // норматив дома (с нормативом)
	CommonTotal *float64   `json:"common_total"` // общий расход общедомовых счётчиков
	ShareBase   *float64   `json:"share_base"`   // сумма площадей, между которыми он делится
	Meters      []MeterUse `json:"meters"`

	amount *big.Rat // точная сумма строки для итога
}

type Result struct {
	AccountID     int64   `json:"account_id"`
	AccountNumber string  `json:"account_number"`
	Period        string  `json:"period"` // YYYY-MM-01
	Lines         []Line  `json:"lines"`
	Total         float64 `json:"total"`    // сумма строк со статусом ok
	Complete      bool    `json:"complete"` // посчитаны все строки
}

type volume struct {
	qty         *big.Rat
	status      string
	meters      []MeterUse
	area        *big.Rat
	norm        *big.Rat
	commonTotal *big.Rat
	shareBase   *big.Rat
	warnings    []string
	failed      string
}

// Compute считает все услуги, назначенные счёту в месяце. Услуга, назначенная в любой день месяца, начисляется за весь месяц.
func Compute(in Input) Result {
	res := Result{AccountID: in.AccountID, AccountNumber: in.AccountNumber, Period: in.Period.Format("2006-01-02"), Lines: []Line{}, Complete: true}
	c := &calculator{in: in, last: in.Period.AddDate(0, 1, -1)}

	// Ручные услуги складывают количества всех назначений месяца, остальные начисляются один раз.
	order := []int64{}
	manual := map[int64]*big.Rat{}
	seen := map[int64]bool{}
	for _, a := range in.Assignments {
		s := in.Services[a.ServiceID]
		if s == nil {
			continue
		}
		if s.Method == "manual" && a.Quantity != nil {
			if manual[a.ServiceID] == nil {
				manual[a.ServiceID] = new(big.Rat)
			}
			manual[a.ServiceID].Add(manual[a.ServiceID], a.Quantity)
		}
		if !seen[a.ServiceID] {
			seen[a.ServiceID] = true
			order = append(order, a.ServiceID)
		}
	}
	sort.SliceStable(order, func(i, j int) bool {
		a, b := in.Services[order[i]], in.Services[order[j]]
		if a.Position != b.Position {
			return a.Position < b.Position
		}
		return a.Name < b.Name
	})

	total := new(big.Rat)
	for _, id := range order {
		line := c.line(in.Services[id], manual[id])
		if line.Status == StatusOK {
			total.Add(total, line.amount)
		} else {
			res.Complete = false
		}
		res.Lines = append(res.Lines, line)
	}
	res.Total = float(total)
	return res
}

type calculator struct {
	in   Input
	last time.Time
}

func (c *calculator) line(s *Service, manualQty *big.Rat) Line {
	name := s.ReceiptName
	if name == "" {
		name = s.Name
	}
	l := Line{ServiceID: s.ID, Name: name, Kind: s.Kind, Unit: s.Unit, CalcMethod: s.Method, Warnings: []string{}, Meters: []MeterUse{}}

	if s.Method == "sum_prices" {
		c.sumPrices(&l, s)
		return l
	}
	v := c.volume(s, manualQty)
	l.Meters, l.Warnings = v.meters, append(l.Warnings, v.warnings...)
	l.Area, l.Norm, l.CommonTotal, l.ShareBase = optFloat(v.area), optFloat(v.norm), optFloat(v.commonTotal), optFloat(v.shareBase)
	l.Failed = v.failed
	if v.status != StatusOK {
		l.Status = v.status
		return l
	}
	if s.PriceFirst == nil {
		l.Status = StatusNoPrice
		return l
	}
	if s.PriceLast != nil && s.PriceLast.Cmp(s.PriceFirst) != 0 {
		l.Warnings = append(l.Warnings, WarnPriceChanges)
	}
	qty := roundRat(v.qty, 4)
	amount := roundRat(new(big.Rat).Mul(qty, s.PriceFirst), 2)
	l.Quantity, l.Price, l.Amount, l.amount = optFloat(qty), optFloat(s.PriceFirst), optFloat(amount), amount
	l.Status = StatusOK
	return l
}

// sumPrices: сумма строк «количество × цена» по составу; количество и цена показываются, только если у всех слагаемых одно количество.
func (c *calculator) sumPrices(l *Line, s *Service) {
	amount := new(big.Rat)
	priceSum := new(big.Rat)
	var common *big.Rat
	same := true
	for _, id := range s.ComponentIDs {
		comp := c.in.Services[id]
		if comp == nil {
			l.Status = StatusComponentFailed
			return
		}
		v := c.volume(comp, nil)
		if v.status != StatusOK {
			l.Status, l.Failed = StatusComponentFailed, comp.Name
			l.Meters = append(l.Meters, v.meters...)
			return
		}
		if comp.PriceFirst == nil {
			l.Status, l.Failed = StatusNoPrice, comp.Name
			return
		}
		if comp.PriceLast != nil && comp.PriceLast.Cmp(comp.PriceFirst) != 0 {
			l.Warnings = append(l.Warnings, WarnPriceChanges)
		}
		q := roundRat(v.qty, 4)
		amount.Add(amount, new(big.Rat).Mul(q, comp.PriceFirst))
		priceSum.Add(priceSum, comp.PriceFirst)
		if common == nil {
			common = q
		} else if common.Cmp(q) != 0 {
			same = false
		}
		l.Meters = append(l.Meters, v.meters...)
		l.Warnings = append(l.Warnings, v.warnings...)
	}
	if len(s.ComponentIDs) == 0 {
		l.Status = StatusComponentFailed
		return
	}
	l.amount = roundRat(amount, 2)
	l.Amount = optFloat(l.amount)
	if same {
		l.Quantity, l.Price = optFloat(common), optFloat(priceSum)
	}
	l.Status = StatusOK
}

func (c *calculator) volume(s *Service, manualQty *big.Rat) volume {
	switch s.Method {
	case "fixed":
		return volume{qty: big.NewRat(1, 1), status: StatusOK}
	case "manual":
		if manualQty == nil {
			return volume{status: StatusNoQuantity}
		}
		return volume{qty: manualQty, status: StatusOK}
	case "per_area":
		if c.in.Area == nil {
			return volume{status: StatusNoArea}
		}
		return volume{qty: c.in.Area, area: c.in.Area, status: StatusOK}
	case "per_area_norm":
		if c.in.Area == nil {
			return volume{status: StatusNoArea}
		}
		if s.NormFirst == nil {
			return volume{status: StatusNoNorm, area: c.in.Area}
		}
		v := volume{qty: new(big.Rat).Mul(c.in.Area, s.NormFirst), area: c.in.Area, norm: s.NormFirst, status: StatusOK}
		if s.NormLast != nil && s.NormLast.Cmp(s.NormFirst) != 0 {
			v.warnings = append(v.warnings, WarnNormChanges)
		}
		return v
	case "individual_meter":
		total, status, uses := c.consumption(c.in.Meters, s)
		return volume{qty: total, status: status, meters: uses}
	case "common_meter":
		return c.common(s)
	case "sum_volumes":
		sum := new(big.Rat)
		var uses []MeterUse
		for _, id := range s.ComponentIDs {
			comp := c.in.Services[id]
			if comp == nil {
				return volume{status: StatusComponentFailed}
			}
			v := c.volume(comp, nil)
			uses = append(uses, v.meters...)
			if v.status != StatusOK {
				return volume{status: StatusComponentFailed, failed: comp.Name, meters: uses}
			}
			sum.Add(sum, roundRat(v.qty, 4))
		}
		if len(s.ComponentIDs) == 0 {
			return volume{status: StatusComponentFailed}
		}
		return volume{qty: sum, status: StatusOK, meters: uses}
	}
	return volume{status: StatusNoQuantity}
}

// common: расход общедомовых счётчиков услуги за месяц делится между помещениями по площади.
func (c *calculator) common(s *Service) volume {
	total, status, uses := c.consumption(s.CommonMeters, s)
	if status != StatusOK {
		return volume{status: status, meters: uses}
	}
	if c.in.Area == nil || s.ShareBase == nil || s.ShareBase.Sign() <= 0 {
		return volume{status: StatusNoArea, meters: uses, area: c.in.Area, commonTotal: total}
	}
	v := volume{
		qty:         new(big.Rat).Quo(new(big.Rat).Mul(total, c.in.Area), s.ShareBase),
		status:      StatusOK,
		meters:      uses,
		area:        c.in.Area,
		commonTotal: total,
		shareBase:   s.ShareBase,
	}
	if s.ShareNoArea > 0 {
		v.warnings = append(v.warnings, WarnAreaMissing)
	}
	return v
}

// consumption суммирует расход счётчиков услуги за месяц. Счётчик учитывается, если работал в месяце, имеет нужный
// ресурс и тариф. Если у какого-то нет показания месяца или предыдущего — суммы нет (частичный расход ввёл бы в заблуждение).
func (c *calculator) consumption(meters []Meter, s *Service) (*big.Rat, string, []MeterUse) {
	tariff := s.MeterTariff
	if tariff < 1 {
		tariff = 1
	}
	total := new(big.Rat)
	uses := []MeterUse{}
	problem := ""
	rank := map[string]int{StatusNegative: 1, StatusNoPrevious: 2, StatusNoReading: 3}
	for _, m := range meters {
		if m.Resource != s.MeterResource || m.Tariffs < tariff {
			continue
		}
		if m.InstalledAt.After(c.last) || (m.RemovedAt != nil && m.RemovedAt.Before(c.in.Period)) {
			continue
		}
		use := MeterUse{MeterID: m.ID, Serial: m.Serial, Name: m.Name, Status: StatusOK}
		idx := tariff - 1
		// Снятый в этом месяце счётчик без показаний за месяц не мешает расчёту: показание при снятии никто не обязан передавать.
		if m.RemovedAt != nil && !m.RemovedAt.After(c.last) && (m.Current == nil || m.Current.T[idx] == nil) {
			use.Status = StatusRetired
			uses = append(uses, use)
			continue
		}
		var cur, prev *big.Rat
		switch {
		case m.Current == nil || m.Current.T[idx] == nil:
			use.Status = StatusNoReading
		default:
			cur = m.Current.T[idx]
			use.Current = optFloat(cur)
			switch {
			case m.Previous != nil && m.Previous.T[idx] != nil:
				prev = m.Previous.T[idx]
			case !m.InstalledAt.Before(c.in.Period): // установлен в этом месяце: отсчёт от начальных показаний
				prev = m.Initial[idx]
				if prev == nil {
					prev = new(big.Rat)
				}
			default:
				use.Status = StatusNoPrevious
			}
		}
		if use.Status == StatusOK {
			use.Previous = optFloat(prev)
			coef := m.Coefficient
			if coef == nil {
				coef = big.NewRat(1, 1)
			}
			d := new(big.Rat).Mul(new(big.Rat).Sub(cur, prev), coef)
			if d.Sign() < 0 {
				use.Status = StatusNegative
			} else {
				use.Consumption = optFloat(d)
				total.Add(total, d)
			}
		}
		if use.Status != StatusOK && rank[use.Status] > rank[problem] {
			problem = use.Status
		}
		uses = append(uses, use)
	}
	counted := 0
	for _, u := range uses {
		if u.Status != StatusRetired {
			counted++
		}
	}
	switch {
	case len(uses) == 0:
		return nil, StatusNoMeters, uses
	case counted == 0: // все счётчики сняты без показаний: считать не из чего
		return nil, StatusNoReading, uses
	case problem != "":
		return nil, problem, uses
	}
	return total, StatusOK, uses
}

// roundRat округляет до n знаков после запятой, половина — от нуля (как округляют деньги).
func roundRat(r *big.Rat, n int) *big.Rat {
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil)
	num := new(big.Int).Mul(r.Num(), scale)
	den := r.Denom()
	neg := num.Sign() < 0
	num.Abs(num)
	// floor((2*num + den) / (2*den))
	num.Mul(num, big.NewInt(2))
	num.Add(num, den)
	q := new(big.Int).Quo(num, new(big.Int).Mul(den, big.NewInt(2)))
	if neg {
		q.Neg(q)
	}
	return new(big.Rat).SetFrac(q, scale)
}

func float(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

func optFloat(r *big.Rat) *float64 {
	if r == nil {
		return nil
	}
	f := float(r)
	return &f
}
