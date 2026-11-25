package model

import "time"

// ResidentAccount — отдельная от сотрудников учётная запись собственника.
type ResidentAccount struct {
	ID           int64  `json:"id" db:"id"`
	PersonID     int64  `json:"person_id" db:"person_id"`
	Email        string `json:"email" db:"email"`
	PasswordHash string `json:"-" db:"password_hash"`
}

type ResidentProperty struct {
	Premises
	Address  string `json:"address" db:"address"`
	KindName string `json:"kind_name" db:"kind_name"`
	ShareNum int    `json:"share_num" db:"share_num"`
	ShareDen int    `json:"share_den" db:"share_den"`
}

// ResidentDebtSummary — задолженность по действующим объектам собственника на дату последнего импорта.
type ResidentDebtSummary struct {
	Period *Date          `json:"period"`
	Total  float64        `json:"total"`
	Items  []ResidentDebt `json:"items"`
}

type ResidentDebt struct {
	Address      string  `json:"address" db:"address"`
	PremisesKind string  `json:"premises_kind" db:"premises_kind"`
	PremisesNo   string  `json:"premises_number" db:"premises_number"`
	Purpose      string  `json:"purpose" db:"purpose"`
	AccountNo    string  `json:"account_number" db:"account_number"`
	Debt         float64 `json:"debt" db:"debt"`
}

type ResidentWindow struct {
	Enabled     bool  `json:"enabled" db:"enabled"`
	ServerDate  Date  `json:"server_date" db:"-"`
	NextOpensOn *Date `json:"next_opens_on" db:"-"`

	BuildingID int64 `json:"building_id" db:"building_id"`
	Period     Date  `json:"period" db:"period"`
	OpensOn    Date  `json:"opens_on" db:"opens_on"`
	ClosesOn   Date  `json:"closes_on" db:"closes_on"`
}

type ResidentReading struct {
	MeterReading
	AuthorID *int64 `json:"-" db:"author_id"`
	Revision int64  `json:"revision" db:"revision"`
	Editable bool   `json:"editable" db:"editable"`
}

// ResidentAccess — сведения для списка физлиц и управления кабинетом, только для администратора.
type ResidentAccess struct {
	PersonID      int64      `json:"person_id" db:"person_id"`
	Email         *string    `json:"email" db:"email"`
	Configured    bool       `json:"configured" db:"configured"`
	Enabled       bool       `json:"enabled" db:"enabled"`
	Registered    bool       `json:"registered" db:"registered"`
	RegisteredAt  *time.Time `json:"registered_at" db:"registered_at"`
	LastLoginAt   *time.Time `json:"last_login_at" db:"last_login_at"`
	AccountActive bool       `json:"account_active" db:"account_active"`
	Eligible      bool       `json:"eligible" db:"eligible"`
}

// ResidentInviteBatchResult — итог массового приглашения действующих собственников.
type ResidentInviteBatchResult struct {
	Invited int                       `json:"invited"`
	Skipped []ResidentInviteBatchSkip `json:"skipped"`
}

type ResidentInviteBatchSkip struct {
	PersonID int64  `json:"person_id"`
	Name     string `json:"name"`
	Reason   string `json:"reason"`
}

// ResidentSchedule — повторяющиеся дни приёма по московскому времени.
type ResidentSchedule struct {
	BuildingID int64 `json:"building_id" db:"building_id"`
	Enabled    bool  `json:"enabled" db:"enabled"`
	OpensDay   int   `json:"opens_day" db:"opens_day"`
	ClosesDay  int   `json:"closes_day" db:"closes_day"`
}

type ResidentScheduleSettings struct {
	ResidentSchedule
	CurrentWindow ResidentWindow   `json:"current_window"`
	Overrides     []ResidentWindow `json:"overrides"`
}

type ResidentReadingInput struct {
	MeterID  int64    `json:"meter_id"`
	Period   Date     `json:"period"`
	T1       *float64 `json:"t1"`
	T2       *float64 `json:"t2"`
	T3       *float64 `json:"t3"`
	Comment  *string  `json:"comment"`
	Revision *int64   `json:"revision"`
}
