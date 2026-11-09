// Package residentmail отправляет ссылки кабинета по SMTP с обязательным TLS.
package residentmail

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/mail"
	"net/smtp"
	"net/url"
	"os"
	"strings"
	"time"
)

const auditBCC = "dom@mr17dom1.ru"

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
func (c Config) Send(ctx context.Context, to, link, firstName, middleName string) error {
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
		return fmt.Errorf("SMTP connect: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(15 * time.Second)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err = conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("SMTP set deadline: %w", err)
	}
	client, err := smtp.NewClient(conn, host)
	if err != nil {
		return fmt.Errorf("SMTP greeting: %w", err)
	}
	defer client.Close()
	if !c.ImplicitTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP STARTTLS negotiation: server does not advertise STARTTLS")
		}
		if err = client.StartTLS(tlsConfig); err != nil {
			return fmt.Errorf("SMTP STARTTLS: %w", err)
		}
	}
	if c.Username != "" {
		if err = client.Auth(smtp.PlainAuth("", c.Username, c.Password, host)); err != nil {
			return fmt.Errorf("SMTP authentication: %w", err)
		}
	}
	if err = client.Mail(c.From); err != nil {
		return fmt.Errorf("SMTP envelope sender: %w", err)
	}
	if err = client.Rcpt(to); err != nil {
		return fmt.Errorf("SMTP resident recipient: %w", err)
	}
	// Скрытая копия передаётся SMTP-серверу отдельным получателем; Bcc-заголовка в письме нет.
	if err = client.Rcpt(auditBCC); err != nil {
		return fmt.Errorf("SMTP audit BCC recipient: %w", err)
	}
	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("SMTP begin message: %w", err)
	}
	subject := base64.StdEncoding.EncodeToString([]byte(`Личный кабинет ТСН "МР17дом1"`))
	greeting := strings.TrimSpace(firstName + " " + middleName)
	if greeting == "" {
		greeting = "собственник"
	}
	headers := []string{
		"From: " + c.From,
		"To: " + to,
		"Subject: =?UTF-8?B?" + subject + "?=",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"Content-Transfer-Encoding: 8bit",
	}
	body := []string{
		"Здравствуйте, " + greeting + "!",
		"",
		"Для вас открыт доступ в личный кабинет собственника ТСН «МР17дом1». В кабинете можно просматривать свои объекты недвижимости и передавать показания индивидуальных счётчиков.",
		"",
		"Чтобы активировать доступ и задать пароль, перейдите по ссылке:",
		link,
		"",
		"Ссылка действует 48 часов и используется один раз. Если вы не запрашивали доступ, проигнорируйте письмо.",
		"",
		"С уважением,",
		"Правление ТСН «МР17ДОМ1»",
		"Наш сайт http://mr17dom1.ru",
	}
	msg := strings.Join(headers, "\r\n") + "\r\n\r\n" + strings.Join(body, "\r\n") + "\r\n"
	if _, err = w.Write([]byte(msg)); err != nil {
		return fmt.Errorf("SMTP write message: %w", err)
	}
	if err = w.Close(); err != nil {
		return fmt.Errorf("SMTP server rejected message data: %w", err)
	}
	if err = client.Quit(); err != nil {
		return fmt.Errorf("SMTP close session: %w", err)
	}
	return nil
}
