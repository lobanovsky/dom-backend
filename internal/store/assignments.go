package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"dom-backend/internal/model"
	"dom-backend/internal/rules"
)

const (
	maxChangeSamples    = 100
	maxUnmatchedSamples = 50
)

// Assignments определяет лицевые счета входящих платежей по правилам: предпросмотр, применение, история, откат.
type Assignments struct{ pool *pgxpool.Pool }

func NewAssignments(pool *pgxpool.Pool) *Assignments { return &Assignments{pool} }

// candidate — платёж, к которому применяются правила, и его текущее состояние.
type candidate struct {
	rules.Payment
	Time       *string
	AccountID  *int64
	CategoryID *int64
	AssignedBy *string
	RuleID     *int64
}

func (c candidate) unassigned() bool { return c.AccountID == nil && c.CategoryID == nil }

type queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadCandidates выбирает платежи по фильтрам. unassigned: без привязки; recompute: ещё и привязанные правилами.
func loadCandidates(ctx context.Context, q queryer, direction, mode string, sc model.AssignmentScope) ([]candidate, error) {
	if direction == model.DirectionOutgoing {
		return loadOutgoingCandidates(ctx, q, mode, sc)
	}
	rows, err := q.Query(ctx,
		`SELECT p.id, p.bank_account_id, b.is_special, p.payment_date, p.amount, p.payer_name, COALESCE(p.payer_inn, ''),
		        COALESCE(p.payer_account, ''), btrim(concat_ws(' ', p.payer_bik, p.payer_bank_name)), COALESCE(p.purpose, ''),
		        COALESCE(p.doc_number, ''), COALESCE(p.comment, ''), COALESCE(p.operation_type, ''),
		        p.personal_account_id, p.category_id, p.assigned_by, p.rule_id
		 FROM incoming_payments p JOIN bank_accounts b ON b.id = p.bank_account_id
		 WHERE p.deleted_at IS NULL
		   AND (($1 = 'recompute' AND (p.assigned_by = 'rule' OR (p.personal_account_id IS NULL AND p.category_id IS NULL)))
		        OR (p.personal_account_id IS NULL AND p.category_id IS NULL))
		   AND ($2::bigint IS NULL OR p.bank_account_id = $2)
		   AND ($3::bigint IS NULL OR p.registry_id = $3)
		   AND ($4::bigint IS NULL OR p.statement_id = $4)
		   AND ($5::date IS NULL OR p.payment_date >= $5)
		   AND ($6::date IS NULL OR p.payment_date <= $6)
		   AND ($7::numeric IS NULL OR p.amount >= $7)
		   AND ($8::numeric IS NULL OR p.amount <= $8)
		   AND ($9::text IS NULL
		        OR concat_ws(' ', p.payer_name, p.purpose, p.comment, p.doc_number, p.external_id) ILIKE $9)
		 ORDER BY p.payment_date, p.id`,
		mode, sc.BankAccountID, sc.RegistryID, sc.StatementID, sc.DateFrom, sc.DateTo, sc.AmountFrom, sc.AmountTo, likePattern(sc.Q))
	if err != nil {
		return nil, mapErr(err)
	}
	return scanCandidates(rows)
}

// loadOutgoingCandidates — то же для исходящих платежей: контрагент — получатель, привязка только к категории.
func loadOutgoingCandidates(ctx context.Context, q queryer, mode string, sc model.AssignmentScope) ([]candidate, error) {
	rows, err := q.Query(ctx,
		`SELECT p.id, p.bank_account_id, b.is_special, p.payment_date, p.amount, p.recipient_name, COALESCE(p.recipient_inn, ''),
		        COALESCE(p.recipient_account, ''), btrim(concat_ws(' ', p.recipient_bik, p.recipient_bank_name)), COALESCE(p.purpose, ''),
		        COALESCE(p.doc_number, ''), COALESCE(p.comment, ''), COALESCE(p.operation_type, ''),
		        NULL::bigint, p.category_id, p.assigned_by, p.rule_id
		 FROM outgoing_payments p JOIN bank_accounts b ON b.id = p.bank_account_id
		 WHERE p.deleted_at IS NULL
		   AND (($1 = 'recompute' AND (p.assigned_by = 'rule' OR p.category_id IS NULL)) OR p.category_id IS NULL)
		   AND ($2::bigint IS NULL OR p.bank_account_id = $2)
		   AND ($3::bigint IS NULL OR p.statement_id = $3)
		   AND ($4::date IS NULL OR p.payment_date >= $4)
		   AND ($5::date IS NULL OR p.payment_date <= $5)
		   AND ($6::numeric IS NULL OR p.amount >= $6)
		   AND ($7::numeric IS NULL OR p.amount <= $7)
		   AND ($8::text IS NULL OR concat_ws(' ', p.recipient_name, p.purpose, p.comment, p.doc_number) ILIKE $8)
		 ORDER BY p.payment_date, p.id`,
		mode, sc.BankAccountID, sc.StatementID, sc.DateFrom, sc.DateTo, sc.AmountFrom, sc.AmountTo, likePattern(sc.Q))
	if err != nil {
		return nil, mapErr(err)
	}
	return scanCandidates(rows)
}

func scanCandidates(rows pgx.Rows) ([]candidate, error) {
	defer rows.Close()
	var out []candidate
	for rows.Next() {
		var c candidate
		var date model.Date
		if err := rows.Scan(&c.ID, &c.BankAccountID, &c.BankSpecial, &date, &c.Amount, &c.PayerName, &c.PayerINN, &c.PayerAccount,
			&c.PayerBank, &c.Purpose, &c.DocNumber, &c.Comment, &c.OperationType, &c.AccountID, &c.CategoryID, &c.AssignedBy, &c.RuleID); err != nil {
			return nil, mapErr(err)
		}
		c.Date = date.Time
		out = append(out, c)
	}
	return out, mapErr(rows.Err())
}

// loadIndex собирает справочники для правил: лицевые счета, помещения, собственники и плательщики счетов.
func loadIndex(ctx context.Context, q queryer) (*rules.Index, error) {
	idx := rules.NewIndex()

	rows, err := q.Query(ctx, `SELECT id, kind, number, building_id FROM premises WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, mapErr(err)
	}
	for rows.Next() {
		var p rules.Premises
		if err := rows.Scan(&p.ID, &p.Kind, &p.Number, &p.BuildingID); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		idx.AddPremises(p)
	}
	rows.Close()

	rows, err = q.Query(ctx, `SELECT id, number, premises_id, purpose FROM personal_accounts WHERE deleted_at IS NULL`)
	if err != nil {
		return nil, mapErr(err)
	}
	for rows.Next() {
		var a rules.Account
		if err := rows.Scan(&a.ID, &a.Number, &a.PremisesID, &a.Purpose); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		if _, ok := idx.PremisesByID[a.PremisesID]; ok {
			idx.AddAccount(a)
		}
	}
	rows.Close()

	// Физлица: собственники и плательщики счетов (помещение плательщика — помещение его счёта).
	people := map[int64]*rules.Person{}
	rows, err = q.Query(ctx,
		`SELECT p.id, concat_ws(' ', p.last_name, p.first_name, p.middle_name), x.premises_id, x.valid_to
		 FROM (SELECT person_id, premises_id, valid_to FROM ownerships WHERE deleted_at IS NULL AND person_id IS NOT NULL
		       UNION ALL
		       SELECT h.person_id, a.premises_id, h.valid_to FROM account_holders h
		         JOIN personal_accounts a ON a.id = h.account_id AND a.deleted_at IS NULL
		        WHERE h.deleted_at IS NULL AND h.person_id IS NOT NULL) x
		 JOIN persons p ON p.id = x.person_id AND p.deleted_at IS NULL`)
	if err != nil {
		return nil, mapErr(err)
	}
	for rows.Next() {
		var id int64
		var name string
		var h rules.Holder
		var to *model.Date
		if err := rows.Scan(&id, &name, &h.PremisesID, &to); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		if _, ok := idx.PremisesByID[h.PremisesID]; !ok {
			continue
		}
		if to != nil {
			t := to.Time
			h.To = &t
		}
		if people[id] == nil {
			people[id] = &rules.Person{Tokens: rules.Tokens(name)}
		}
		people[id].Holders = append(people[id].Holders, h)
	}
	rows.Close()
	for _, p := range people {
		idx.People = append(idx.People, *p)
	}

	// Юрлица по ИНН.
	rows, err = q.Query(ctx,
		`SELECT le.inn, x.premises_id, x.valid_to
		 FROM (SELECT legal_entity_id, premises_id, valid_to FROM ownerships WHERE deleted_at IS NULL AND legal_entity_id IS NOT NULL
		       UNION ALL
		       SELECT h.legal_entity_id, a.premises_id, h.valid_to FROM account_holders h
		         JOIN personal_accounts a ON a.id = h.account_id AND a.deleted_at IS NULL
		        WHERE h.deleted_at IS NULL AND h.legal_entity_id IS NOT NULL) x
		 JOIN legal_entities le ON le.id = x.legal_entity_id AND le.deleted_at IS NULL`)
	if err != nil {
		return nil, mapErr(err)
	}
	for rows.Next() {
		var inn string
		var h rules.Holder
		var to *model.Date
		if err := rows.Scan(&inn, &h.PremisesID, &to); err != nil {
			rows.Close()
			return nil, mapErr(err)
		}
		if _, ok := idx.PremisesByID[h.PremisesID]; !ok {
			continue
		}
		if to != nil {
			t := to.Time
			h.To = &t
		}
		idx.EntitiesByINN[strings.TrimSpace(inn)] = append(idx.EntitiesByINN[strings.TrimSpace(inn)], h)
	}
	rows.Close()
	return idx, nil
}

func loadRules(ctx context.Context, q queryer, direction string) ([]model.PaymentRule, error) {
	rows, err := q.Query(ctx, `SELECT `+ruleCols+` FROM payment_rules WHERE deleted_at IS NULL AND enabled AND direction = $1 ORDER BY position, id`, direction)
	if err != nil {
		return nil, mapErr(err)
	}
	return pgx.CollectRows(rows, pgx.RowToStructByName[model.PaymentRule])
}

type computation struct {
	cands    []candidate
	outcomes []rules.Outcome
	changes  []string
	idx      *rules.Index
	ruleName map[int64]string
	warnings []string
}

// compute применяет правила к платежам и определяет, что с каждым случится. only — проверка одного правила.
func compute(ctx context.Context, q queryer, req model.AssignRequest) (*computation, error) {
	c := &computation{ruleName: map[int64]string{}}
	var err error
	if c.cands, err = loadCandidates(ctx, q, req.Direction, req.Mode, req.Scope); err != nil {
		return nil, err
	}
	if req.Direction == model.DirectionOutgoing {
		c.idx = rules.NewIndex() // исходящим нужна только категория: справочник лицевых счетов не нужен
	} else if c.idx, err = loadIndex(ctx, q); err != nil {
		return nil, err
	}
	all, err := loadRules(ctx, q, req.Direction)
	if err != nil {
		return nil, err
	}
	use := all
	switch {
	case req.Rule != nil:
		draft := *req.Rule
		draft.Enabled, draft.Meta.ID = true, -1
		use = []model.PaymentRule{draft}
	case req.RuleID != nil:
		rows, err := q.Query(ctx, `SELECT `+ruleCols+` FROM payment_rules WHERE id = $1 AND deleted_at IS NULL AND direction = $2`, *req.RuleID, req.Direction)
		if err != nil {
			return nil, mapErr(err)
		}
		found, err := pgx.CollectRows(rows, pgx.RowToStructByName[model.PaymentRule])
		if err != nil {
			return nil, mapErr(err)
		}
		if len(found) == 0 {
			return nil, &Error{ErrNotFound, "rule not found"}
		}
		found[0].Enabled = true // проверка работает и для выключенного правила
		use = found
	}
	for _, r := range use {
		c.ruleName[r.ID] = r.Name
	}
	engine, errs := rules.New(use, c.idx)
	for _, e := range errs {
		c.warnings = append(c.warnings, e.Error())
	}
	for _, cand := range c.cands {
		out := engine.Resolve(cand.Payment)
		c.outcomes = append(c.outcomes, out)
		c.changes = append(c.changes, classify(cand, out))
	}
	return c, nil
}

func classify(c candidate, out rules.Outcome) string {
	switch {
	case out.Resolved() && c.unassigned():
		return model.ChangeNew
	case out.Resolved() && sameTarget(c, out):
		return model.ChangeSame
	case out.Resolved():
		return model.ChangeChanged
	case c.unassigned():
		return model.ChangeUnresolved
	}
	return model.ChangeCleared
}

func sameTarget(c candidate, o rules.Outcome) bool {
	eq := func(a, b *int64) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
	return eq(c.AccountID, o.AccountID) && eq(c.CategoryID, o.CategoryID) && c.RuleID != nil && *c.RuleID == o.RuleID
}

// prepare проверяет запрос и подставляет направление по умолчанию (входящие).
func prepare(req *model.AssignRequest) error {
	if req.Direction == "" {
		req.Direction = model.DirectionIncoming
	}
	if req.Direction != model.DirectionIncoming && req.Direction != model.DirectionOutgoing {
		return invalid("direction: must be one of: incoming, outgoing")
	}
	if req.Mode != model.AssignUnassigned && req.Mode != model.AssignRecompute {
		return invalid("mode: must be one of: unassigned, recompute")
	}
	return nil
}

// Preview показывает, что сделают правила, ничего не записывая.
func (s *Assignments) Preview(ctx context.Context, req model.AssignRequest) (model.AssignPreview, error) {
	if err := prepare(&req); err != nil {
		return model.AssignPreview{}, err
	}
	if req.Rule != nil {
		if req.Rule.Direction == "" {
			req.Rule.Direction = req.Direction
		}
		if req.Rule.Direction != req.Direction {
			return model.AssignPreview{}, invalid("rule.direction: must match direction of the request")
		}
		req.Rule.SetDefaults()
		if err := req.Rule.Validate(); err != nil {
			return model.AssignPreview{}, err
		}
	}
	c, err := compute(ctx, s.pool, req)
	if err != nil {
		return model.AssignPreview{}, err
	}
	names, err := s.lookupNames(ctx)
	if err != nil {
		return model.AssignPreview{}, err
	}
	return c.preview(names), nil
}

type nameLookup struct {
	categories map[int64]string
}

func (s *Assignments) lookupNames(ctx context.Context) (nameLookup, error) {
	n := nameLookup{categories: map[int64]string{}}
	rows, err := s.pool.Query(ctx, `SELECT id, name FROM payment_categories`)
	if err != nil {
		return n, mapErr(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return n, mapErr(err)
		}
		n.categories[id] = name
	}
	return n, mapErr(rows.Err())
}

func (c *computation) preview(names nameLookup) model.AssignPreview {
	p := model.AssignPreview{Candidates: len(c.cands), ByRule: []model.RuleCount{}, Reasons: []model.ReasonCount{},
		Samples: []model.AssignSample{}, Unmatched: []model.AssignSample{}, Warnings: c.warnings}
	if p.Warnings == nil {
		p.Warnings = []string{}
	}
	byRule := map[int64]int{}
	reasons := map[string]int{}
	for i, cand := range c.cands {
		out, change := c.outcomes[i], c.changes[i]
		switch change {
		case model.ChangeNew:
			p.New++
		case model.ChangeChanged:
			p.Changed++
		case model.ChangeSame:
			p.Same++
		case model.ChangeCleared:
			p.Cleared++
		case model.ChangeUnresolved:
			p.Unresolved++
		}
		if out.Resolved() {
			byRule[out.RuleID]++
		}
		if change == model.ChangeUnresolved || change == model.ChangeCleared {
			reasons[out.Reason]++
		}
		sample := c.sample(cand, out, change, names)
		switch {
		case change == model.ChangeUnresolved && len(p.Unmatched) < maxUnmatchedSamples:
			p.Unmatched = append(p.Unmatched, sample)
		case (change == model.ChangeNew || change == model.ChangeChanged || change == model.ChangeCleared) && len(p.Samples) < maxChangeSamples:
			p.Samples = append(p.Samples, sample)
		}
	}
	for id, n := range byRule {
		p.ByRule = append(p.ByRule, model.RuleCount{RuleID: id, RuleName: c.ruleName[id], Count: n})
	}
	sort.Slice(p.ByRule, func(i, j int) bool { return p.ByRule[i].Count > p.ByRule[j].Count })
	for r, n := range reasons {
		p.Reasons = append(p.Reasons, model.ReasonCount{Reason: r, Count: n})
	}
	sort.Slice(p.Reasons, func(i, j int) bool { return p.Reasons[i].Count > p.Reasons[j].Count })
	return p
}

func (c *computation) sample(cand candidate, out rules.Outcome, change string, names nameLookup) model.AssignSample {
	s := model.AssignSample{PaymentID: cand.ID, PaymentDate: model.Date{Time: cand.Date}, PayerName: cand.PayerName, Amount: cand.Amount,
		Purpose: truncate(cand.Purpose, 200), Change: change, Reason: out.Reason}
	if out.Resolved() {
		s.RuleID, s.RuleName = out.RuleID, c.ruleName[out.RuleID]
		if out.AccountID != nil {
			s.AccountNumber = c.idx.AccountsByID[*out.AccountID].Number
		}
		if out.CategoryID != nil {
			s.CategoryName = names.categories[*out.CategoryID]
		}
	}
	if cand.AccountID != nil {
		s.PrevAccount = c.idx.AccountsByID[*cand.AccountID].Number
	} else if cand.CategoryID != nil {
		s.PrevAccount = names.categories[*cand.CategoryID]
	}
	return s
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// Apply применяет правила одной транзакцией и записывает запуск с прежним состоянием платежей (для отката).
// Платежи с привязкой «вручную» или «из реестра» не затрагиваются. Если менять нечего, запуск не создаётся.
func (s *Assignments) Apply(ctx context.Context, req model.AssignRequest) (model.AssignResult, error) {
	if err := prepare(&req); err != nil {
		return model.AssignResult{}, err
	}
	if req.Rule != nil || req.RuleID != nil {
		return model.AssignResult{}, invalid("rule: applying a single rule is not supported, use preview")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return model.AssignResult{}, err
	}
	defer rollback(ctx, tx)

	c, err := compute(ctx, tx, req)
	if err != nil {
		return model.AssignResult{}, err
	}
	res := model.AssignResult{Candidates: len(c.cands)}
	for _, ch := range c.changes {
		switch ch {
		case model.ChangeNew:
			res.New++
		case model.ChangeChanged:
			res.Changed++
		case model.ChangeCleared:
			res.Cleared++
		case model.ChangeUnresolved:
			res.Unresolved++
		}
	}
	if res.New+res.Changed+res.Cleared == 0 {
		return res, nil
	}

	filters, _ := json.Marshal(req.Scope)
	if err := tx.QueryRow(ctx,
		`INSERT INTO assignment_runs (mode, filters, candidates, assigned_count, changed_count, cleared_count, direction)
		 VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		req.Mode, string(filters), res.Candidates, res.New+res.Changed, res.Changed, res.Cleared, req.Direction).Scan(&res.RunID); err != nil {
		return model.AssignResult{}, mapErr(err)
	}
	for i, cand := range c.cands {
		change := c.changes[i]
		if change != model.ChangeNew && change != model.ChangeChanged && change != model.ChangeCleared {
			continue
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO assignment_run_items (run_id, payment_id, prev_personal_account, prev_category, prev_assigned_by, prev_rule_id, prev_run_id)
			 SELECT $1, id, `+prevAccountExpr(req.Direction)+`, category_id, assigned_by, rule_id, run_id FROM `+paymentsTable(req.Direction)+` WHERE id = $2`,
			res.RunID, cand.ID); err != nil {
			return model.AssignResult{}, mapErr(err)
		}
		out := c.outcomes[i]
		var assignedBy *string
		var ruleID *int64
		if out.Resolved() {
			by := "rule"
			assignedBy, ruleID = &by, &out.RuleID
		}
		if _, err := tx.Exec(ctx, updateAssignmentSQL(req.Direction),
			cand.ID, out.AccountID, out.CategoryID, assignedBy, ruleID, res.RunID); err != nil {
			return model.AssignResult{}, mapErr(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return model.AssignResult{}, mapErr(err)
	}
	return res, nil
}

func paymentsTable(direction string) string {
	if direction == model.DirectionOutgoing {
		return "outgoing_payments"
	}
	return "incoming_payments"
}

// prevAccountExpr — что сохранять как прежний лицевой счёт: у исходящих платежей его нет.
func prevAccountExpr(direction string) string {
	if direction == model.DirectionOutgoing {
		return "NULL::bigint"
	}
	return "personal_account_id"
}

// updateAssignmentSQL записывает результат правила; у исходящих платежей нет лицевого счёта ($2 не используется, но параметр передаётся).
func updateAssignmentSQL(direction string) string {
	if direction == model.DirectionOutgoing {
		return `UPDATE outgoing_payments SET category_id = $3, assigned_by = $4, rule_id = $5, run_id = $6, updated_at = now()
		        WHERE id = $1 AND deleted_at IS NULL AND $2::bigint IS NULL`
	}
	return `UPDATE incoming_payments SET personal_account_id = $2, category_id = $3, assigned_by = $4, rule_id = $5, run_id = $6, updated_at = now()
	        WHERE id = $1 AND deleted_at IS NULL`
}

// Rollback возвращает прежнее состояние платежей запуска. Платежи, привязку которых после запуска изменили вручную
// или другой запуск, не трогаются (kept).
func (s *Assignments) Rollback(ctx context.Context, runID int64) (model.RollbackResult, error) {
	var res model.RollbackResult
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return res, err
	}
	defer rollback(ctx, tx)

	var rolledBack *time.Time
	var direction string
	err = tx.QueryRow(ctx, `SELECT rolled_back_at, direction FROM assignment_runs WHERE id = $1 FOR UPDATE`, runID).Scan(&rolledBack, &direction)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, &Error{ErrNotFound, "assignment run not found"}
	}
	if err != nil {
		return res, mapErr(err)
	}
	if rolledBack != nil {
		return res, &Error{ErrConflict, "assignment run is already rolled back"}
	}
	restore := `UPDATE incoming_payments p
		 SET personal_account_id = i.prev_personal_account, category_id = i.prev_category, assigned_by = i.prev_assigned_by,
		     rule_id = i.prev_rule_id, run_id = i.prev_run_id, updated_at = now()
		 FROM assignment_run_items i
		 WHERE i.run_id = $1 AND p.id = i.payment_id AND p.run_id = $1`
	if direction == model.DirectionOutgoing {
		restore = `UPDATE outgoing_payments p
		 SET category_id = i.prev_category, assigned_by = i.prev_assigned_by,
		     rule_id = i.prev_rule_id, run_id = i.prev_run_id, updated_at = now()
		 FROM assignment_run_items i
		 WHERE i.run_id = $1 AND p.id = i.payment_id AND p.run_id = $1`
	}
	tag, err := tx.Exec(ctx, restore, runID)
	if err != nil {
		return res, mapErr(err)
	}
	res.Restored = int(tag.RowsAffected())
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM assignment_run_items WHERE run_id = $1`, runID).Scan(&total); err != nil {
		return res, mapErr(err)
	}
	res.Kept = total - res.Restored
	if _, err := tx.Exec(ctx, `UPDATE assignment_runs SET rolled_back_at = now(), rolled_back_kept = $2 WHERE id = $1`, runID, res.Kept); err != nil {
		return res, mapErr(err)
	}
	if err := tx.Commit(ctx); err != nil {
		return res, mapErr(err)
	}
	return res, nil
}

// Runs возвращает историю запусков, новые сверху.
// direction: incoming | outgoing; пусто — все.
func (s *Assignments) Runs(ctx context.Context, direction string, limit, offset int) ([]model.AssignRun, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, direction, mode, filters, candidates, assigned_count, changed_count, cleared_count, created_at, rolled_back_at, rolled_back_kept
		 FROM assignment_runs WHERE ($3 = '' OR direction = $3) ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset, direction)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	out := []model.AssignRun{}
	for rows.Next() {
		var r model.AssignRun
		var filters []byte
		if err := rows.Scan(&r.ID, &r.Direction, &r.Mode, &filters, &r.Candidates, &r.AssignedCount, &r.ChangedCount, &r.ClearedCount, &r.CreatedAt, &r.RolledBackAt, &r.RolledBackKept); err != nil {
			return nil, mapErr(err)
		}
		if err := json.Unmarshal(filters, &r.Scope); err != nil {
			return nil, fmt.Errorf("run %d filters: %w", r.ID, err)
		}
		out = append(out, r)
	}
	return out, mapErr(rows.Err())
}
