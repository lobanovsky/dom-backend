package mailfetch

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// Config — подключение к ящику. Password — пароль для внешнего приложения (у mail.ru обычный пароль не подходит).
type Config struct {
	Addr     string // host:port, IMAP поверх TLS (imap.mail.ru:993)
	User     string
	Password string
	Folder   string
	From     string      // необязательно: только письма от этого отправителя
	TLS      *tls.Config // для тестов с самоподписанным сертификатом; по умолчанию проверка сертификата сервера
}

// Session — открытая на чтение папка. После работы вызвать Close.
type Session struct {
	c           *imapclient.Client
	cfg         Config
	uidValidity uint32
}

// Open подключается, входит и открывает папку только на чтение (EXAMINE).
func Open(ctx context.Context, cfg Config) (*Session, error) {
	type result struct {
		s   *Session
		err error
	}
	done := make(chan result, 1)
	go func() {
		s, err := open(cfg)
		done <- result{s, err}
	}()
	select {
	case r := <-done:
		return r.s, r.err
	case <-ctx.Done():
		go func() { // дождаться и закрыть, если подключение всё же удалось
			if r := <-done; r.s != nil {
				r.s.Close()
			}
		}()
		return nil, ctx.Err()
	}
}

func open(cfg Config) (*Session, error) {
	tlsCfg := cfg.TLS
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	c, err := imapclient.DialTLS(cfg.Addr, &imapclient.Options{TLSConfig: tlsCfg})
	if err != nil {
		return nil, fmt.Errorf("imap connect: %w", err)
	}
	if err := c.Login(cfg.User, cfg.Password).Wait(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("imap login: %w", err) // текст ошибки сервера не содержит пароля
	}
	data, err := c.Select(cfg.Folder, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("imap folder %q: %w", cfg.Folder, err)
	}
	return &Session{c: c, cfg: cfg, uidValidity: data.UIDValidity}, nil
}

// UIDValidity — признак «поколения» номеров писем папки; если он изменился, прежние UID недействительны.
func (s *Session) UIDValidity() uint32 { return s.uidValidity }

func (s *Session) Close() {
	_ = s.c.Logout().Wait()
	_ = s.c.Close()
}

// Search возвращает UID писем, полученных не раньше since (точность до суток, как у IMAP SINCE), по возрастанию.
func (s *Session) Search(since time.Time) ([]uint32, error) {
	crit := &imap.SearchCriteria{Since: since, Smaller: MaxMessageSize}
	if s.cfg.From != "" {
		crit.Header = []imap.SearchCriteriaHeaderField{{Key: "From", Value: s.cfg.From}}
	}
	data, err := s.c.UIDSearch(crit, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("imap search: %w", err)
	}
	var out []uint32
	for _, u := range data.AllUIDs() {
		out = append(out, uint32(u))
	}
	return out, nil
}

// Fetch скачивает письмо целиком, не меняя его флагов (BODY.PEEK[]), и разбирает.
func (s *Session) Fetch(uid uint32) (Message, error) {
	section := &imap.FetchItemBodySection{Peek: true}
	msgs, err := s.c.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{UID: true, BodySection: []*imap.FetchItemBodySection{section}}).Collect()
	if err != nil {
		return Message{}, fmt.Errorf("imap fetch uid %d: %w", uid, err)
	}
	if len(msgs) == 0 {
		return Message{}, fmt.Errorf("imap fetch uid %d: %w", uid, ErrGone)
	}
	raw := msgs[0].FindBodySection(section)
	if raw == nil {
		return Message{}, fmt.Errorf("imap fetch uid %d: empty body", uid)
	}
	m, err := ParseMessage(raw)
	m.UID = uid
	return m, err
}

// ErrGone — письмо исчезло из папки между поиском и загрузкой (удалено или перемещено).
var ErrGone = errors.New("message is gone")
