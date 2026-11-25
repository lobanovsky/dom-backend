package residentmail

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"net/mail"
	"strings"
	"testing"
	"time"
)

func TestHotWaterMessage(t *testing.T) {
	pdf := []byte("%PDF-1.4\n")
	raw, err := hotWaterMessage("sender@example.test", "to@example.test", []string{"cc@example.test"}, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), pdf)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil || !strings.HasSuffix(subject, "Ноябрь 2026") || msg.Header.Get("Cc") != "cc@example.test" || msg.Header.Get("Bcc") != "" {
		t.Fatalf("unexpected message headers: %v", msg.Header)
	}
	media, params, err := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if err != nil || media != "multipart/mixed" {
		t.Fatalf("invalid message content type: %q (%v)", media, err)
	}
	parts := multipart.NewReader(msg.Body, params["boundary"])
	body, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	text, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, body))
	if err != nil || !strings.Contains(string(text), "Правление ТСН «МР17ДОМ1»") {
		t.Fatalf("unexpected body: %q (%v)", text, err)
	}
	attachment, err := parts.NextPart()
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, attachment))
	if err != nil || attachment.Header.Get("Content-Type") != "application/pdf" || !bytes.Equal(data, pdf) {
		t.Fatalf("unexpected PDF attachment: %v", err)
	}
}
