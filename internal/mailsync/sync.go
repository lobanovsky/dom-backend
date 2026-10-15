// Package mailsync забирает реестры платежей из письма в папке почты и загружает их обычным импортом реестров.
// Письма не меняются; разобранные помнятся в БД (по UID папки), чтобы не скачивать их повторно.
package mailsync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"dom-backend/internal/mailfetch"
	"dom-backend/internal/model"
	"dom-backend/internal/registryimport"
)

// ErrBusy — другой запуск ещё идёт.
var ErrBusy = errors.New("mail sync is already running")

// maxDetails — сколько файлов запуска записывается в журнал поимённо.
const maxDetails = 200

// Session — открытая папка ящика (в проде mailfetch.Session).
type Session interface {
	UIDValidity() uint32
	Search(since time.Time) ([]uint32, error)
	Fetch(uid uint32) (mailfetch.Message, error)
	Close()
}

// Mailbox открывает папку. В проде — обёртка над mailfetch.Open.
type Mailbox interface {
	Open(ctx context.Context) (Session, error)
}

type Importer interface {
	Accounts(ctx context.Context) (registryimport.Accounts, error)
	Import(ctx context.Context, acc registryimport.Accounts, f registryimport.File) (registryimport.FileResult, string)
}

type Journal interface {
	TryLock(ctx context.Context) (func(), bool, error)
	StartRun(ctx context.Context, trigger string) (int64, error)
	FinishRun(ctx context.Context, id int64, r model.MailRun) error
	Processed(ctx context.Context, folder string, uidValidity uint32) (map[uint32]bool, error)
	MarkProcessed(ctx context.Context, m model.MailMessage) error
}

// Detail — результат по одному файлу из письма (пишется в журнал запуска).
type Detail struct {
	Subject    string `json:"subject"`
	From       string `json:"from"`
	Date       string `json:"date,omitempty"`
	File       string `json:"file"`
	Status     string `json:"status"` // статусы registryimport, а также registryimport.IgnoreNoAccount
	RegistryID int64  `json:"registry_id,omitempty"`
	Payments   int    `json:"payments,omitempty"`
	Error      string `json:"error,omitempty"`
}

type Syncer struct {
	Mailbox   Mailbox
	Importer  Importer
	Journal   Journal
	Folder    string // имя папки: часть ключа обработанных писем
	SinceDays int
	Log       *slog.Logger
	now       func() time.Time
}

func (s *Syncer) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

type run struct {
	s       *Syncer
	id      int64
	trigger string
	unlock  func()
}

func (s *Syncer) begin(ctx context.Context, trigger string) (*run, error) {
	unlock, ok, err := s.Journal.TryLock(ctx)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrBusy
	}
	id, err := s.Journal.StartRun(ctx, trigger)
	if err != nil {
		unlock()
		return nil, err
	}
	return &run{s: s, id: id, trigger: trigger, unlock: unlock}, nil
}

// Sync забирает реестры из новых писем и ждёт окончания.
func (s *Syncer) Sync(ctx context.Context, trigger string) (model.MailRun, error) {
	r, err := s.begin(ctx, trigger)
	if err != nil {
		return model.MailRun{}, err
	}
	return r.exec(ctx), nil
}

// Start запускает забор в фоне и возвращает номер запуска; ход и итог видны в журнале. ctx — время жизни сервера, а не запроса.
func (s *Syncer) Start(ctx context.Context, trigger string) (int64, error) {
	r, err := s.begin(ctx, trigger)
	if err != nil {
		return 0, err
	}
	go r.exec(ctx)
	return r.id, nil
}

// Run проверяет почту раз в every, пока не отменён ctx. Нулевой every — расписания нет.
func (s *Syncer) Run(ctx context.Context, every time.Duration) {
	if every <= 0 {
		return
	}
	s.Log.Info("mail sync scheduled", "every", every.String())
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		// Первая проверка — сразу после старта: письма могли прийти, пока сервер стоял.
		if _, err := s.Sync(ctx, "schedule"); err != nil && !errors.Is(err, context.Canceled) {
			if errors.Is(err, ErrBusy) {
				s.Log.Info("mail sync skipped: another run is in progress")
			} else {
				s.Log.Error("mail sync failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (r *run) exec(ctx context.Context) model.MailRun {
	s := r.s
	defer r.unlock()
	res := model.MailRun{ID: r.id, Trigger: r.trigger}
	details := []Detail{}
	var errs []string
	fail := func(format string, args ...any) {
		msg := fmt.Sprintf(format, args...)
		s.Log.Error("mail sync", "run", r.id, "err", msg)
		errs = append(errs, msg)
	}
	s.process(ctx, &res, &details, fail)
	if len(details) > maxDetails {
		details = details[:maxDetails]
	}
	res.Details, _ = json.Marshal(details)
	if len(errs) > 0 {
		e := strings.Join(errs, "; ")
		res.Error = &e
	}
	s.Log.Info("mail sync", "run", r.id, "trigger", r.trigger, "messages", res.Messages, "files", res.Files,
		"imported", res.Imported, "payments", res.Payments, "duplicates", res.Duplicates, "failed", res.Failed, "errors", len(errs))
	_ = s.Journal.FinishRun(context.WithoutCancel(ctx), r.id, res)
	return res
}

func (s *Syncer) process(ctx context.Context, res *model.MailRun, details *[]Detail, fail func(string, ...any)) {
	sess, err := s.Mailbox.Open(ctx)
	if err != nil {
		fail("%v", err)
		return
	}
	defer sess.Close()
	acc, err := s.Importer.Accounts(ctx)
	if err != nil {
		fail("bank accounts: %v", err)
		return
	}
	validity := sess.UIDValidity()
	done, err := s.Journal.Processed(ctx, s.Folder, validity)
	if err != nil {
		fail("processed messages: %v", err)
		return
	}
	uids, err := sess.Search(s.clock().AddDate(0, 0, -s.SinceDays))
	if err != nil {
		fail("%v", err)
		return
	}
	for _, uid := range uids {
		if done[uid] {
			continue
		}
		if ctx.Err() != nil {
			fail("stopped: %v", ctx.Err())
			return
		}
		msg, err := sess.Fetch(uid)
		if err != nil {
			if !errors.Is(err, mailfetch.ErrGone) {
				fail("%v", err)
			}
			continue
		}
		res.Messages++
		files, retry := s.handleMessage(ctx, acc, msg, res, details)
		if retry {
			continue // повторим при следующем запуске, пока письмо в окне поиска
		}
		err = s.Journal.MarkProcessed(ctx, model.MailMessage{
			Folder: s.Folder, UIDValidity: validity, UID: uid, MessageID: msg.MessageID,
			From: msg.From, Subject: msg.Subject, ReceivedAt: msg.Date, Files: files,
		})
		if err != nil {
			fail("mark message %d: %v", uid, err)
		}
	}
}

// handleMessage загружает реестры из вложений письма. retry=true — письмо нельзя считать разобранным: у файла нет
// известного счёта (после добавления кода реестра он загрузится при следующем запуске) или сбой был временным.
func (s *Syncer) handleMessage(ctx context.Context, acc registryimport.Accounts, msg mailfetch.Message, res *model.MailRun, details *[]Detail) (files int, retry bool) {
	add := func(file, status string, fr registryimport.FileResult) {
		d := Detail{Subject: msg.Subject, From: msg.From, File: file, Status: status, RegistryID: fr.RegistryID, Error: fr.Error}
		if !msg.Date.IsZero() {
			d.Date = msg.Date.Format(time.RFC3339)
		}
		if fr.Result != nil {
			d.RegistryID, d.Payments = fr.Result.RegistryID, fr.Result.Created
		}
		*details = append(*details, d)
	}
	for _, att := range msg.Attachments {
		cands, err := registryimport.Expand(att.Name, att.Data)
		if err != nil {
			files++
			res.Failed++
			add(att.Name, registryimport.Error, registryimport.FileResult{Error: err.Error()})
			continue
		}
		for _, f := range cands {
			files++
			fr, ignore := s.Importer.Import(ctx, acc, f)
			switch {
			case ignore == registryimport.IgnoreNoAccount:
				res.Failed++
				retry = true
				add(f.Name, registryimport.IgnoreNoAccount, registryimport.FileResult{Error: "no bank account with this registry code"})
				continue
			case ignore != "":
				files-- // не реестр
				continue
			}
			add(f.Name, fr.Status, fr)
			switch fr.Status {
			case registryimport.Imported:
				res.Imported++
				res.Payments += fr.Result.Created
			case registryimport.Duplicate, registryimport.AllDuplicates:
				res.Duplicates++
			case registryimport.UnknownAccount:
				res.Failed++
				retry = true
			case registryimport.Error:
				res.Failed++
				retry = retry || fr.Error == "internal error"
			default: // invalid: файл испорчен, повтор не поможет
				res.Failed++
			}
		}
	}
	res.Files += files
	return files, retry
}
