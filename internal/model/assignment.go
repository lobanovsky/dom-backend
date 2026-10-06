package model

import "time"

// Режимы определения лицевых счетов.
const (
	AssignUnassigned = "unassigned" // только платежи без привязки
	AssignRecompute  = "recompute"  // ещё и пересчитать то, что раньше определили правила
)

// AssignRequest — запрос на предпросмотр или применение правил.
type AssignRequest struct {
	Mode  string          `json:"mode"`
	Scope AssignmentScope `json:"scope"`
	// Проверка одного правила: сохранённого (rule_id) или черновика из формы (rule). Только для предпросмотра.
	RuleID *int64       `json:"rule_id,omitempty"`
	Rule   *PaymentRule `json:"rule,omitempty"`
}

// Что случилось бы с платежом (или случилось).
const (
	ChangeNew        = "new"        // платёж без привязки получил её
	ChangeChanged    = "changed"    // привязка от правила изменилась
	ChangeSame       = "same"       // привязка от правила осталась прежней
	ChangeCleared    = "cleared"    // привязка от правила снята: теперь ни одно правило не подходит
	ChangeUnresolved = "unresolved" // платёж остался без привязки
)

type AssignSample struct {
	PaymentID     int64   `json:"payment_id"`
	PaymentDate   Date    `json:"payment_date"`
	PayerName     string  `json:"payer_name"`
	Amount        float64 `json:"amount"`
	Purpose       string  `json:"purpose"`
	Change        string  `json:"change"`
	RuleID        int64   `json:"rule_id,omitempty"`
	RuleName      string  `json:"rule_name,omitempty"`
	AccountNumber string  `json:"account_number,omitempty"`
	CategoryName  string  `json:"category_name,omitempty"`
	PrevAccount   string  `json:"prev_account,omitempty"`
	Reason        string  `json:"reason,omitempty"`
}

type RuleCount struct {
	RuleID   int64  `json:"rule_id"`
	RuleName string `json:"rule_name"`
	Count    int    `json:"count"`
}

type ReasonCount struct {
	Reason string `json:"reason"`
	Count  int    `json:"count"`
}

// AssignPreview — итог предпросмотра: ничего не записано.
type AssignPreview struct {
	Candidates int            `json:"candidates"` // платежей, к которым применялись правила
	New        int            `json:"new"`
	Changed    int            `json:"changed"`
	Same       int            `json:"same"`
	Cleared    int            `json:"cleared"`
	Unresolved int            `json:"unresolved"`
	ByRule     []RuleCount    `json:"by_rule"`
	Reasons    []ReasonCount  `json:"reasons"`
	Samples    []AssignSample `json:"samples"`   // изменения (первые)
	Unmatched  []AssignSample `json:"unmatched"` // не определённые (первые)
	Warnings   []string       `json:"warnings"`
}

// AssignRun — запуск определения (в истории).
type AssignRun struct {
	ID             int64           `json:"id"`
	Mode           string          `json:"mode"`
	Scope          AssignmentScope `json:"scope"`
	Candidates     int             `json:"candidates"`
	AssignedCount  int             `json:"assigned_count"`
	ChangedCount   int             `json:"changed_count"`
	ClearedCount   int             `json:"cleared_count"`
	CreatedAt      time.Time       `json:"created_at"`
	RolledBackAt   *time.Time      `json:"rolled_back_at"`
	RolledBackKept int             `json:"rolled_back_kept"`
}

// AssignResult — итог применения. RunID == 0, если менять было нечего (запуск не создаётся).
type AssignResult struct {
	RunID      int64 `json:"run_id"`
	Candidates int   `json:"candidates"`
	New        int   `json:"new"`
	Changed    int   `json:"changed"`
	Cleared    int   `json:"cleared"`
	Unresolved int   `json:"unresolved"`
}

// RollbackResult — итог отката: восстановлено платежей и оставлено (их привязку после запуска изменили вручную или другим запуском).
type RollbackResult struct {
	Restored int `json:"restored"`
	Kept     int `json:"kept"`
}
