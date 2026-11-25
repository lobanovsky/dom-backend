package residentmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"mime"
	"mime/multipart"
	"net/textproto"
	"strings"
	"time"
)

func (c Config) SendHotWaterReport(ctx context.Context, period time.Time, pdf []byte) error {
	const to = "OS-3@mail.ru"
	cc := []string{"vva1969@gmail.com", "shurygin_dm@mail.ru"}
	msg, err := hotWaterMessage(c.From, to, cc, period, pdf)
	if err != nil {
		return err
	}
	return c.sendMessage(ctx, append([]string{to}, cc...), msg)
}

func hotWaterMessage(from, to string, cc []string, period time.Time, pdf []byte) ([]byte, error) {
	months := [...]string{"Январь", "Февраль", "Март", "Апрель", "Май", "Июнь", "Июль", "Август", "Сентябрь", "Октябрь", "Ноябрь", "Декабрь"}
	subject := fmt.Sprintf(`Справка об объемах горячей воды. ТСН "МР17дом1". %s %d`, months[period.Month()-1], period.Year())
	var msg bytes.Buffer
	parts := multipart.NewWriter(&msg)
	fmt.Fprintf(&msg, "From: %s\r\nTo: %s\r\nCc: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=%q\r\n\r\n",
		from, to, strings.Join(cc, ", "), mime.BEncoding.Encode("UTF-8", subject), parts.Boundary())
	writePart := func(headers textproto.MIMEHeader, data []byte) error {
		headers.Set("Content-Transfer-Encoding", "base64")
		part, err := parts.CreatePart(headers)
		if err != nil {
			return err
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		for len(encoded) > 76 {
			if _, err := fmt.Fprint(part, encoded[:76], "\r\n"); err != nil {
				return err
			}
			encoded = encoded[76:]
		}
		_, err = fmt.Fprint(part, encoded)
		return err
	}
	body := strings.Join([]string{
		"Добрый день!",
		"Направляем справку об объемах горячей воды, адрес дома: Москва, 17-й проезд Марьиной Рощи, дом 1",
		"", "С уважением,", "Правление ТСН «МР17ДОМ1»", "Наш сайт http://mr17dom1.ru",
	}, "\r\n")
	if err := writePart(textproto.MIMEHeader{"Content-Type": {"text/plain; charset=UTF-8"}}, []byte(body)); err != nil {
		return nil, err
	}
	name := period.Format("2006-01") + "-Справка об объемах горячей воды.pdf"
	if err := writePart(textproto.MIMEHeader{
		"Content-Type":        {"application/pdf"},
		"Content-Disposition": {mime.FormatMediaType("attachment", map[string]string{"filename": name})},
	}, pdf); err != nil {
		return nil, err
	}
	if err := parts.Close(); err != nil {
		return nil, err
	}
	return msg.Bytes(), nil
}
