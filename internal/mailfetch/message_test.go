package mailfetch

import (
	"encoding/base64"
	"strings"
	"testing"
)

func b64(s string) string {
	e := base64.StdEncoding.EncodeToString([]byte(s))
	var b strings.Builder
	for len(e) > 76 { // как в настоящих письмах: строки по 76 символов
		b.WriteString(e[:76] + "\r\n")
		e = e[76:]
	}
	return b.String() + e
}

func TestParseMessage(t *testing.T) {
	reg := strings.Repeat("1;2;3\r\n", 40)
	raw := strings.ReplaceAll(`From: =?UTF-8?B?0JLQotCR?= <bank@vtb.example>
To: me@mail.example
Subject: =?UTF-8?B?0KDQtdC10YHRgtGA?= 030626
Date: Fri, 03 Jun 2026 10:11:12 +0300
Message-ID: <abc@vtb.example>
MIME-Version: 1.0
Content-Type: multipart/mixed; boundary="B1"

--B1
Content-Type: multipart/alternative; boundary="B2"

--B2
Content-Type: text/plain; charset=utf-8
Content-Transfer-Encoding: quoted-printable

=D0=A0=D0=B5=D0=B5=D1=81=D1=82=D1=80 =D0=BA =D0=BF=D0=B8=D1=81=D1=8C=D0=BC=D1=83
--B2
Content-Type: text/html; charset=utf-8

<p>hi</p>
--B2--
--B1
Content-Type: text/plain; name="=?UTF-8?B?0YDQtdC10YHRgtGALnR4dA==?="
Content-Disposition: attachment; filename="030626_VTB_231068_001.txt"
Content-Transfer-Encoding: base64

`+b64(reg)+`
--B1
Content-Type: application/zip
Content-Disposition: attachment; filename*=UTF-8''%D1%80%D0%B5%D0%B5%D1%81%D1%82%D1%80.zip
Content-Transfer-Encoding: base64

`+b64("PK-not-really-zip")+`
--B1
Content-Type: message/rfc822
Content-Disposition: attachment; filename="fwd.eml"

Subject: nested

--B1--
`, "\n", "\r\n")

	m, err := ParseMessage([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.From != "bank@vtb.example" || m.Subject != "Реестр 030626" || m.MessageID != "<abc@vtb.example>" || m.Date.IsZero() {
		t.Errorf("headers: %+v", m)
	}
	if len(m.Attachments) != 2 {
		t.Fatalf("attachments = %d, want 2 (текст письма и вложенное письмо не вложения): %+v", len(m.Attachments), m.Attachments)
	}
	if a := m.Attachments[0]; a.Name != "030626_VTB_231068_001.txt" || string(a.Data) != reg {
		t.Errorf("first attachment = %q (%d bytes)", a.Name, len(a.Data))
	}
	if a := m.Attachments[1]; a.Name != "реестр.zip" || string(a.Data) != "PK-not-really-zip" {
		t.Errorf("second attachment = %q %q", a.Name, a.Data)
	}
}

func TestParseMessageWithoutAttachments(t *testing.T) {
	m, err := ParseMessage([]byte("From: a@b.example\r\nSubject: hi\r\n\r\njust text\r\n"))
	if err != nil || len(m.Attachments) != 0 || m.Subject != "hi" {
		t.Errorf("%v %+v", err, m)
	}
	if _, err := ParseMessage([]byte("")); err == nil {
		t.Error("пустое письмо должно давать ошибку")
	}
}

func TestParseMessageSingleAttachmentBody(t *testing.T) {
	raw := "From: a@b.example\r\nContent-Type: text/plain; name=\"r.txt\"\r\nContent-Disposition: attachment; filename=\"r.txt\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" + b64("abc") + "\r\n"
	m, err := ParseMessage([]byte(raw))
	if err != nil || len(m.Attachments) != 1 || string(m.Attachments[0].Data) != "abc" {
		t.Errorf("%v %+v", err, m)
	}
}
