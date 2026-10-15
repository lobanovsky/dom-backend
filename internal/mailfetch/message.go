// Package mailfetch читает письма из папки IMAP и достаёт из них вложения. Ящик открывается только на чтение,
// тело берётся через BODY.PEEK[]: флаги писем (в том числе «прочитано») не меняются.
package mailfetch

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
)

// Пределы разбора: письмо читается в память целиком.
const (
	MaxMessageSize    = 30 << 20 // письмо целиком
	maxAttachmentSize = 25 << 20 // одно вложение после декодирования
	maxDepth          = 6        // вложенность multipart
)

type Attachment struct {
	Name string
	Data []byte
}

// Message — письмо: заголовки и вложения (части с именем файла). UID заполняет клиент.
type Message struct {
	UID         uint32
	MessageID   string
	From        string
	Subject     string
	Date        time.Time
	Attachments []Attachment
}

var wordDecoder = &mime.WordDecoder{}

func decodeHeader(s string) string {
	if d, err := wordDecoder.DecodeHeader(s); err == nil {
		return d
	}
	return s
}

// ParseMessage разбирает письмо (RFC 5322 с MIME). Имена вложений декодируются (RFC 2047 и RFC 2231);
// части без имени файла (текст письма) и вложенные письма пропускаются.
func ParseMessage(raw []byte) (Message, error) {
	m, err := mail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		return Message{}, fmt.Errorf("cannot parse the message: %w", err)
	}
	out := Message{
		MessageID: strings.TrimSpace(m.Header.Get("Message-Id")),
		Subject:   decodeHeader(m.Header.Get("Subject")),
	}
	if d, err := m.Header.Date(); err == nil {
		out.Date = d
	}
	if from, err := m.Header.AddressList("From"); err == nil && len(from) > 0 {
		out.From = from[0].Address
	} else {
		out.From = decodeHeader(m.Header.Get("From"))
	}
	err = walk(m.Header.Get("Content-Type"), m.Header.Get("Content-Disposition"), m.Header.Get("Content-Transfer-Encoding"), m.Body, 0, &out)
	return out, err
}

// walk обходит часть письма: multipart — рекурсивно, остальные — вложение, если у части есть имя файла.
func walk(contentType, disposition, encoding string, body io.Reader, depth int, out *Message) error {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType, params = "text/plain", nil
	}
	if strings.HasPrefix(mediaType, "multipart/") {
		if depth >= maxDepth || params["boundary"] == "" {
			return nil
		}
		mr := multipart.NewReader(body, params["boundary"])
		for {
			p, err := mr.NextPart()
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return fmt.Errorf("cannot read the message parts: %w", err)
			}
			// multipart.Part сам раскодирует quoted-printable и уберёт этот заголовок; base64 остаётся нам.
			if err := walk(p.Header.Get("Content-Type"), p.Header.Get("Content-Disposition"), p.Header.Get("Content-Transfer-Encoding"), p, depth+1, out); err != nil {
				return err
			}
		}
	}
	if strings.HasPrefix(mediaType, "message/") {
		return nil
	}
	name := attachmentName(disposition, params)
	if name == "" {
		return nil
	}
	var r io.Reader = body
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, newlineSkipper{body})
	case "quoted-printable":
		// Только у корневой части без multipart: у частей multipart декодирование уже сделано.
		if depth == 0 {
			r = quotedprintable.NewReader(body)
		}
	}
	data, err := io.ReadAll(io.LimitReader(r, maxAttachmentSize+1))
	if err != nil {
		return fmt.Errorf("cannot decode the attachment %q: %w", name, err)
	}
	if len(data) > maxAttachmentSize {
		return fmt.Errorf("attachment %q is too large (max %d MB)", name, maxAttachmentSize>>20)
	}
	out.Attachments = append(out.Attachments, Attachment{Name: name, Data: data})
	return nil
}

// attachmentName берёт имя файла из Content-Disposition, а при его отсутствии — из параметра name типа содержимого.
func attachmentName(disposition string, typeParams map[string]string) string {
	name := ""
	if _, p, err := mime.ParseMediaType(disposition); err == nil {
		name = p["filename"]
	}
	if name == "" {
		name = typeParams["name"]
	}
	return strings.TrimSpace(decodeHeader(name))
}

// newlineSkipper убирает переводы строк и пробелы, из-за которых base64.StdEncoding отвергает письма (строки по 76 символов).
type newlineSkipper struct{ r io.Reader }

func (n newlineSkipper) Read(p []byte) (int, error) {
	for {
		c, err := n.r.Read(p)
		j := 0
		for i := 0; i < c; i++ {
			if p[i] != '\r' && p[i] != '\n' && p[i] != ' ' && p[i] != '\t' {
				p[j] = p[i]
				j++
			}
		}
		if j > 0 || err != nil {
			return j, err
		}
	}
}
