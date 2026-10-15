package store

import (
	"context"
	"testing"
	"time"

	"dom-backend/internal/model"
)

func TestMailJournal(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)
	s := NewMail(pool)
	const folder = "ut_folder"
	cleanup := func() { pool.Exec(ctx, `DELETE FROM mail_messages WHERE folder = $1`, folder) } //nolint:errcheck // best-effort test cleanup
	cleanup()
	t.Cleanup(cleanup)

	// Блокировка: второй запуск не стартует, пока не освобождена первая.
	unlock, ok, err := s.TryLock(ctx)
	if err != nil || !ok {
		t.Fatalf("TryLock: %v %v", ok, err)
	}
	if _, ok2, _ := s.TryLock(ctx); ok2 {
		t.Error("вторая блокировка не должна браться")
	}
	unlock()
	if unlock2, ok3, _ := s.TryLock(ctx); !ok3 {
		t.Error("после unlock блокировка должна браться")
	} else {
		unlock2()
	}

	id, err := s.StartRun(ctx, "manual")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.FinishRun(ctx, id, model.MailRun{Messages: 2, Files: 3, Imported: 1, Payments: 10, Duplicates: 1, Failed: 1,
		Details: []byte(`[{"file":"a.txt","status":"imported"}]`)}); err != nil {
		t.Fatal(err)
	}
	runs, err := s.Runs(ctx, 5)
	if err != nil || len(runs) == 0 || runs[0].ID != id || runs[0].FinishedAt == nil || runs[0].Payments != 10 || runs[0].Error != nil {
		t.Fatalf("Runs: %v %+v", err, runs)
	}

	// Разобранные письма различаются по папке, UIDVALIDITY и UID.
	msg := model.MailMessage{Folder: folder, UIDValidity: 5, UID: 11, Subject: "Реестр", ReceivedAt: time.Now(), Files: 2}
	for i := 0; i < 2; i++ { // повторная запись безопасна
		if err := s.MarkProcessed(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Processed(ctx, folder, 5)
	if err != nil || len(got) != 1 || !got[11] {
		t.Errorf("Processed: %v %v", err, got)
	}
	if other, _ := s.Processed(ctx, folder, 6); len(other) != 0 {
		t.Errorf("другой UIDVALIDITY должен давать пустой набор: %v", other)
	}
}
