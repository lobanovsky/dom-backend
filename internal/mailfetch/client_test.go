package mailfetch

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"math/big"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

func selfSigned(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, DNSNames: []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func letter(from, subject, attName string) string {
	return strings.ReplaceAll("From: "+from+"\nTo: me@example.com\nSubject: "+subject+"\nDate: "+time.Now().Format(time.RFC1123Z)+
		"\nMIME-Version: 1.0\nContent-Type: multipart/mixed; boundary=B\n\n--B\nContent-Type: text/plain\n\ntext\n--B\n"+
		"Content-Type: text/plain\nContent-Disposition: attachment; filename=\""+attName+"\"\nContent-Transfer-Encoding: base64\n\nYWJj\n--B--\n", "\n", "\r\n")
}

// IMAP-сервер в памяти: проверяем реальный обмен с библиотекой — папка открывается на чтение,
// письма находятся по отправителю, читаются целиком, а флаг «прочитано» не появляется.
func TestSession(t *testing.T) {
	mem := imapmemserver.New()
	user := imapmemserver.NewUser("reg@example.com", "app-password")
	if err := user.Create("VTB", nil); err != nil {
		t.Fatal(err)
	}
	for _, l := range []string{
		letter("VTB Bank <bank@vtb.example>", "Reestr 1", "030626_VTB_231068_001.txt"),
		letter("someone@else.example", "Not ours", "x.txt"),
		letter("bank@vtb.example", "Reestr 2", "030626_VTB_231068_002.txt"),
	} {
		if _, err := user.Append("VTB", bytesLiteral(l), &imap.AppendOptions{Time: time.Now()}); err != nil {
			t.Fatal(err)
		}
	}
	mem.AddUser(user)

	cert := selfSigned(t)
	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		InsecureAuth: true,
	})
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	pool := x509.NewCertPool()
	leaf, _ := x509.ParseCertificate(cert.Certificate[0])
	pool.AddCert(leaf)
	cfg := Config{Addr: ln.Addr().String(), User: "reg@example.com", Password: "app-password", Folder: "VTB", From: "vtb.example",
		TLS: &tls.Config{RootCAs: pool, ServerName: "localhost"}}

	s, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.UIDValidity() == 0 {
		t.Error("UIDVALIDITY не получен")
	}
	uids, err := s.Search(time.Now().AddDate(0, 0, -1))
	if err != nil || len(uids) != 2 {
		t.Fatalf("Search по отправителю: %v %v (ждём 2 письма банка)", err, uids)
	}
	m, err := s.Fetch(uids[0])
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Reestr 1" || m.UID != uids[0] || len(m.Attachments) != 1 || m.Attachments[0].Name != "030626_VTB_231068_001.txt" || string(m.Attachments[0].Data) != "abc" {
		t.Errorf("письмо: %+v", m)
	}
	if _, err := s.Fetch(999); !errors.Is(err, ErrGone) {
		t.Errorf("несуществующее письмо: %v, want ErrGone", err)
	}

	// Ящик не менялся: после чтения письма остались непрочитанными.
	again, err := Open(context.Background(), Config{Addr: cfg.Addr, User: cfg.User, Password: cfg.Password, Folder: "VTB", TLS: cfg.TLS})
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	data, err := again.c.UIDSearch(&imap.SearchCriteria{NotFlag: []imap.Flag{imap.FlagSeen}}, nil).Wait()
	if err != nil || len(data.AllUIDs()) != 3 {
		t.Errorf("непрочитанных писем = %d, want 3 (флаг «прочитано» ставить нельзя): %v", len(data.AllUIDs()), err)
	}
}

func TestOpenErrors(t *testing.T) {
	// Неверные учётные данные и отсутствие сервера дают понятную ошибку без пароля.
	_, err := Open(context.Background(), Config{Addr: "127.0.0.1:1", User: "u", Password: "secret-pass", Folder: "INBOX"})
	if err == nil || strings.Contains(err.Error(), "secret-pass") {
		t.Errorf("ошибка подключения: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Open(ctx, Config{Addr: "127.0.0.1:1", User: "u", Password: "p", Folder: "INBOX"}); err == nil {
		t.Error("отменённый контекст должен давать ошибку")
	}
}

func bytesLiteral(s string) imap.LiteralReader {
	return literalOf{bytes.NewReader([]byte(s)), int64(len(s))}
}

type literalOf struct {
	*bytes.Reader
	n int64
}

func (l literalOf) Size() int64 { return l.n }
