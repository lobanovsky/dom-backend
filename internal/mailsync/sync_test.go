package mailsync

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"dom-backend/internal/mailfetch"
	"dom-backend/internal/model"
	"dom-backend/internal/registryimport"
)

type fakeSession struct {
	messages  map[uint32]mailfetch.Message
	fetched   []uint32
	closed    bool
	searchErr error
}

func (f *fakeSession) UIDValidity() uint32 { return 7 }
func (f *fakeSession) Search(time.Time) ([]uint32, error) {
	var uids []uint32
	for u := uint32(1); u <= 10; u++ {
		if _, ok := f.messages[u]; ok {
			uids = append(uids, u)
		}
	}
	return uids, f.searchErr
}
func (f *fakeSession) Fetch(uid uint32) (mailfetch.Message, error) {
	f.fetched = append(f.fetched, uid)
	m, ok := f.messages[uid]
	if !ok {
		return m, mailfetch.ErrGone
	}
	m.UID = uid
	return m, nil
}
func (f *fakeSession) Close() { f.closed = true }

type fakeMailbox struct {
	s   *fakeSession
	err error
}

func (m fakeMailbox) Open(context.Context) (Session, error) { return m.s, m.err }

// fakeImporter: файл «known_*.txt» грузится, «dup_*.txt» уже загружен, «nocode_*.txt» — нет кода реестра, «acc_*.txt» — счёт неизвестен.
type fakeImporter struct{ imported []string }

func (f *fakeImporter) Accounts(context.Context) (registryimport.Accounts, error) {
	return registryimport.Accounts{}, nil
}
func (f *fakeImporter) Import(_ context.Context, _ registryimport.Accounts, file registryimport.File) (registryimport.FileResult, string) {
	switch {
	case has(file.Name, "known_"):
		f.imported = append(f.imported, file.Name)
		return registryimport.FileResult{FileName: file.Name, Status: registryimport.Imported, Result: &model.RegistryImportResult{RegistryID: 5, Created: 3}}, ""
	case has(file.Name, "dup_"):
		return registryimport.FileResult{FileName: file.Name, Status: registryimport.Duplicate, RegistryID: 4}, ""
	case has(file.Name, "bad_"):
		return registryimport.FileResult{FileName: file.Name, Status: registryimport.Invalid, Error: "bad footer"}, ""
	case has(file.Name, "acc_"):
		return registryimport.FileResult{FileName: file.Name, Status: registryimport.UnknownAccount}, ""
	}
	return registryimport.FileResult{}, registryimport.IgnoreNoAccount
}

func has(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

type fakeJournal struct {
	mu        sync.Mutex
	locked    bool
	processed map[uint32]bool
	marked    []model.MailMessage
	finished  []model.MailRun
}

func (j *fakeJournal) TryLock(context.Context) (func(), bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.locked {
		return nil, false, nil
	}
	j.locked = true
	return func() { j.mu.Lock(); j.locked = false; j.mu.Unlock() }, true, nil
}
func (j *fakeJournal) StartRun(context.Context, string) (int64, error) { return 1, nil }
func (j *fakeJournal) FinishRun(_ context.Context, _ int64, r model.MailRun) error {
	j.finished = append(j.finished, r)
	return nil
}
func (j *fakeJournal) Processed(context.Context, string, uint32) (map[uint32]bool, error) {
	out := map[uint32]bool{}
	for u := range j.processed {
		out[u] = true
	}
	return out, nil
}
func (j *fakeJournal) MarkProcessed(_ context.Context, m model.MailMessage) error {
	if j.processed == nil {
		j.processed = map[uint32]bool{}
	}
	j.processed[m.UID] = true
	j.marked = append(j.marked, m)
	return nil
}

func msg(files ...string) mailfetch.Message {
	m := mailfetch.Message{Subject: "Реестр", From: "bank@example.com"}
	for _, f := range files {
		m.Attachments = append(m.Attachments, mailfetch.Attachment{Name: f, Data: []byte("x")})
	}
	return m
}

func newSyncer(s *fakeSession, j *fakeJournal) (*Syncer, *fakeImporter) {
	imp := &fakeImporter{}
	return &Syncer{Mailbox: fakeMailbox{s: s}, Importer: imp, Journal: j, Folder: "VTB", SinceDays: 30,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}, imp
}

func TestSync(t *testing.T) {
	sess := &fakeSession{messages: map[uint32]mailfetch.Message{
		1: msg("known_1.txt", "dup_2.txt"), // загружен + дубль → письмо разобрано
		2: msg("scan.pdf"),                 // нет реестров → разобрано, больше не скачивается
		3: msg("nocode_3.txt"),             // нет кода реестра у счёта → повтор позже
		4: msg("acc_4.txt"),                // счёт не в системе → повтор позже
		5: msg("bad_5.txt"),                // испорчен → повтор не поможет
		6: msg(),                           // письмо без вложений
	}}
	j := &fakeJournal{processed: map[uint32]bool{6: true}} // 6 уже разобрано раньше
	s, imp := newSyncer(sess, j)

	res, err := s.Sync(context.Background(), "manual")
	if err != nil {
		t.Fatal(err)
	}
	if res.Messages != 5 || res.Files != 5 || res.Imported != 1 || res.Payments != 3 || res.Duplicates != 1 || res.Failed != 3 {
		t.Errorf("итог: %+v", res)
	}
	if res.Error != nil {
		t.Errorf("ошибок запуска быть не должно: %s", *res.Error)
	}
	for _, uid := range sess.fetched {
		if uid == 6 {
			t.Error("уже разобранное письмо скачано повторно")
		}
	}
	want := map[uint32]bool{1: true, 2: true, 5: true, 6: true}
	for uid := range want {
		if !j.processed[uid] {
			t.Errorf("письмо %d должно быть помечено разобранным", uid)
		}
	}
	for _, uid := range []uint32{3, 4} {
		if j.processed[uid] {
			t.Errorf("письмо %d (нет счёта) нельзя помечать разобранным: после добавления кода его нужно подхватить", uid)
		}
	}
	if !sess.closed {
		t.Error("сессия не закрыта")
	}
	var details []Detail
	if err := json.Unmarshal(j.finished[0].Details, &details); err != nil || len(details) != 5 {
		t.Fatalf("details: %v %s", err, j.finished[0].Details)
	}
	if details[0].File != "known_1.txt" || details[0].Status != registryimport.Imported || details[0].RegistryID != 5 || details[0].Payments != 3 {
		t.Errorf("details[0] = %+v", details[0])
	}

	// Второй запуск: скачиваются только письма, ожидающие счёта.
	sess.fetched = nil
	imp.imported = nil
	res, err = s.Sync(context.Background(), "schedule")
	if err != nil || res.Messages != 2 || len(sess.fetched) != 2 {
		t.Errorf("повторный запуск: %v %+v fetched=%v", err, res, sess.fetched)
	}
}

func TestSyncErrors(t *testing.T) {
	j := &fakeJournal{}
	s := &Syncer{Mailbox: fakeMailbox{err: errors.New("imap login: bad credentials")}, Importer: &fakeImporter{}, Journal: j, Folder: "VTB", SinceDays: 1,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	res, err := s.Sync(context.Background(), "manual")
	if err != nil || res.Error == nil || *res.Error != "imap login: bad credentials" || len(j.finished) != 1 {
		t.Errorf("ошибка подключения должна попасть в журнал: %v %+v", err, res)
	}
}

func TestSyncBusy(t *testing.T) {
	j := &fakeJournal{locked: true}
	s, _ := newSyncer(&fakeSession{}, j)
	if _, err := s.Start(context.Background(), "manual"); !errors.Is(err, ErrBusy) {
		t.Errorf("Start при занятой блокировке: %v", err)
	}
}
