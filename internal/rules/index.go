package rules

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Account — действующий лицевой счёт.
type Account struct {
	ID         int64
	Number     string
	PremisesID int64
	Purpose    string // utilities | capital_repair | parking | other
}

// Premises — помещение.
type Premises struct {
	ID         int64
	Kind       string
	Number     string
	BuildingID int64
}

// Holder — помещение, которым владеет или за которое платит лицо; To == nil — без ограничения по времени.
// Начало периода не учитывается намеренно: даты владения у загруженных данных часто равны дате загрузки.
type Holder struct {
	PremisesID int64
	To         *time.Time
}

// Person — физлицо: слова ФИО (нормализованные, отсортированные) и помещения.
type Person struct {
	Tokens  []string
	Holders []Holder
}

// Index — справочники для действий правил. Заполняется хранилищем один раз на запуск.
type Index struct {
	AccountsByNumber   map[string]Account
	AccountsByID       map[int64]Account
	AccountsByPremises map[int64][]Account
	PremisesByID       map[int64]Premises
	PremisesByKey      map[string][]int64 // «вид|номер» -> помещения (по домам)
	People             []Person
	EntitiesByINN      map[string][]Holder
}

func NewIndex() *Index {
	return &Index{
		AccountsByNumber:   map[string]Account{},
		AccountsByID:       map[int64]Account{},
		AccountsByPremises: map[int64][]Account{},
		PremisesByID:       map[int64]Premises{},
		PremisesByKey:      map[string][]int64{},
		EntitiesByINN:      map[string][]Holder{},
	}
}

func (i *Index) AddAccount(a Account) {
	i.AccountsByNumber[a.Number] = a
	i.AccountsByID[a.ID] = a
	i.AccountsByPremises[a.PremisesID] = append(i.AccountsByPremises[a.PremisesID], a)
}

func (i *Index) AddPremises(p Premises) {
	i.PremisesByID[p.ID] = p
	key := p.Kind + "|" + NormText(p.Number)
	i.PremisesByKey[key] = append(i.PremisesByKey[key], p.ID)
}

func (i *Index) HasAccount(id int64) bool { _, ok := i.AccountsByID[id]; return ok }

// AccountOfPremises выбирает лицевой счёт помещения по типу банковского счёта: на спецсчёт капремонта — счёт капремонта,
// на обычный — счёт ЖКУ (при его отсутствии единственный не капитальный). Неоднозначность — отказ с причиной.
func (i *Index) AccountOfPremises(premisesID int64, special bool) (*int64, string) {
	if _, ok := i.PremisesByID[premisesID]; !ok {
		return nil, "помещение из правила не найдено или удалено"
	}
	var utilities, other, capital []Account
	for _, a := range i.AccountsByPremises[premisesID] {
		switch a.Purpose {
		case "capital_repair":
			capital = append(capital, a)
		case "utilities":
			utilities = append(utilities, a)
		default:
			other = append(other, a)
		}
	}
	pick := func(list []Account, what string) (*int64, string) {
		switch len(list) {
		case 0:
			return nil, "у помещения нет лицевого счёта: " + what
		case 1:
			id := list[0].ID
			return &id, ""
		}
		return nil, "у помещения несколько лицевых счетов: " + what
	}
	if special {
		return pick(capital, "капремонт")
	}
	if len(utilities) > 0 {
		return pick(utilities, "ЖКУ")
	}
	return pick(other, "ЖКУ")
}

// PremisesByNumber ищет помещение по виду и номеру (в доме building, если задан). Единственное совпадение — успех.
func (i *Index) PremisesByNumber(kind, number string, building *int64) (int64, string) {
	var found []int64
	for _, id := range i.PremisesByKey[kind+"|"+NormText(number)] {
		if building == nil || i.PremisesByID[id].BuildingID == *building {
			found = append(found, id)
		}
	}
	switch len(found) {
	case 1:
		return found[0], ""
	case 0:
		return 0, fmt.Sprintf("помещение № %s не найдено", strings.TrimSpace(number))
	}
	return 0, fmt.Sprintf("помещение № %s есть в нескольких домах", strings.TrimSpace(number))
}

var (
	flatHint    = regexp.MustCompile(`(?i)(?:кв(?:артир[а-я]*)?\.?)\s*№?\s*(\d{1,4})`)
	parkingHint = regexp.MustCompile(`(?i)(?:м\s*/\s*м|м-м|мм|машино[\s-]*мест[а-я]*|а\s*/\s*м|парковочн[а-я]*\s+мест[а-я]*)\s*[.№]*\s*(\d{1,4})`)
)

// numberHints достаёт из текста назначения номера квартир и машиномест: «кв. 107», «м/м 138», «ММ63», «машиноместо №34».
func numberHints(text string) (flats, parking []string) {
	for _, m := range flatHint.FindAllStringSubmatch(text, -1) {
		flats = append(flats, m[1])
	}
	for _, m := range parkingHint.FindAllStringSubmatch(text, -1) {
		parking = append(parking, m[1])
	}
	return
}

func holderPremises(hs []Holder, date time.Time, into map[int64]bool) {
	for _, h := range hs {
		if h.To == nil || !h.To.Before(date) {
			into[h.PremisesID] = true
		}
	}
}

// PremisesByPayer ищет помещение по ФИО плательщика среди собственников и плательщиков лицевых счетов, а при
// отсутствии ФИО по ИНН юрлица. У человека несколько помещений: номер квартиры или машиноместа берётся из назначения.
func (i *Index) PremisesByPayer(p Payment) (int64, string) {
	candidates := map[int64]bool{}
	tokens := Tokens(p.PayerName)
	named := false
	for _, person := range i.People {
		if NamesMatch(tokens, person.Tokens) {
			named = true
			holderPremises(person.Holders, p.Date, candidates)
		}
	}
	if !named && p.PayerINN != "" {
		holderPremises(i.EntitiesByINN[strings.TrimSpace(p.PayerINN)], p.Date, candidates)
		named = len(candidates) > 0
	}
	if !named {
		return 0, "плательщик не найден среди собственников и плательщиков счетов"
	}
	switch len(candidates) {
	case 0:
		return 0, "у найденного плательщика нет действующих помещений"
	case 1:
		for id := range candidates {
			flats, parking := numberHints(p.Purpose)
			// Указан номер другого помещения: платёж, вероятно, не за помещение плательщика.
			if (len(flats) > 0 || len(parking) > 0) && !hintMatches(i.PremisesByID[id], flats, parking) {
				return 0, "в назначении указано помещение, которое не принадлежит плательщику"
			}
			return id, ""
		}
	}
	flats, parking := numberHints(p.Purpose)
	var chosen []int64
	for id := range candidates {
		if hintMatches(i.PremisesByID[id], flats, parking) {
			chosen = append(chosen, id)
		}
	}
	if len(chosen) == 1 {
		return chosen[0], ""
	}
	if len(flats) == 0 && len(parking) == 0 {
		return 0, "у плательщика несколько помещений, в назначении нет номера квартиры или машиноместа"
	}
	return 0, "у плательщика несколько помещений, номер из назначения подходит не однозначно"
}

func hintMatches(p Premises, flats, parking []string) bool {
	list := flats
	if p.Kind == "parking_space" {
		list = parking
	}
	for _, n := range list {
		if NormText(n) == NormText(p.Number) {
			return true
		}
	}
	return false
}
