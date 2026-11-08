// Package residentmail отправляет ссылки кабинета по SMTP с обязательным TLS.
package residentmail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

type Config struct {
	BaseURL, Addr, Username, Password, From string
	ImplicitTLS                             bool
}

func Load() (Config, error) {
	c := Config{BaseURL: strings.TrimRight(os.Getenv("RESIDENT_BASE_URL"), "/"), Addr: os.Getenv("RESIDENT_SMTP_ADDR"), Username: os.Getenv("MAIL_USER"), From: os.Getenv("RESIDENT_MAIL_FROM"), ImplicitTLS: os.Getenv("RESIDENT_SMTP_TLS") == "implicit"}
	if c.Addr == "" {
		return c, nil
	}
	u, e := url.Parse(c.BaseURL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" {
		return c, fmt.Errorf("RESIDENT_BASE_URL must be an HTTPS origin")
	}
	if a, e := mail.ParseAddress(c.From); e != nil || a.Address != c.From {
		return c, fmt.Errorf("RESIDENT_MAIL_FROM must be an email address")
	}
	if _, _, e = net.SplitHostPort(c.Addr); e != nil {
		return c, fmt.Errorf("RESIDENT_SMTP_ADDR must be host:port")
	}
	if f := os.Getenv("MAIL_PASSWORD_FILE"); f != "" {
		b, e := os.ReadFile(f)
		if e != nil {
			return c, fmt.Errorf("cannot read MAIL_PASSWORD_FILE")
		}
		c.Password = strings.TrimSpace(string(b))
	}
	if c.Username != "" && c.Password == "" {
		return c, fmt.Errorf("MAIL_PASSWORD_FILE is required for SMTP with MAIL_USER")
	}
	return c, nil
}
func (c Config) Enabled() bool { return c.Addr != "" }
func (c Config) Send(ctx context.Context, to, link string) error {
	host, _, err := net.SplitHostPort(c.Addr)
	if err != nil {
		return err
	}
	dial := net.Dialer{Timeout: 10 * time.Second}
	var conn net.Conn
	tlsConfig := &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}
	if c.ImplicitTLS {
		conn, err = (&tls.Dialer{NetDialer: &dial, Config: tlsConfig}).DialContext(ctx, "tcp", c.Addr)
	} else {
		conn, err = dial.DialContext(ctx, "tcp", c.Addr)
	}
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return err
	}
	defer client.Close()
	if !c.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP requires STARTTLS")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return err
		}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, host)); err != nil {
			return err
		}
	}
	if err = client.Mail(c.From); err != nil {
		return err
	}
	if err = client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	msg := "From: " + c.From + "\r\nTo: " + to + "\r\nSubject: =?UTF-8?B?0JvQuNGH0L3Ri9C5INC60LDQsdC40L3QtdGC?=\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\nДля установки пароля откройте ссылку:\r\n" + link + "\r\n\r\nСсылка действует 30 минут и используется один раз. Если вы не запрашивали доступ, проигнорируйте письмо.\r\n"
	if _, err = w.Write([]byte(msg)); err != nil {
		return err
	}
	if err = w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
