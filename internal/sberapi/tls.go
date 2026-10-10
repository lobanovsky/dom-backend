package sberapi

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

// TLSFiles — откуда брать клиентский сертификат и доверенные корневые.
type TLSFiles struct {
	P12          string // клиентский сертификат с ключом (.p12)
	PasswordFile string // пароль от .p12 одной строкой
	CADir        string // каталог с сертификатами Сбера (*.cer, *.crt, *.pem); необязательно
}

// NewHTTPClient собирает HTTP-клиент с mTLS для Sber API. Второе значение — срок действия клиентского сертификата.
func NewHTTPClient(f TLSFiles) (*http.Client, time.Time, error) {
	data, err := os.ReadFile(f.P12)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("sber tls: %w", err)
	}
	pass, err := os.ReadFile(f.PasswordFile)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("sber tls password: %w", err)
	}
	der, err := berToDER(data)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("sber tls: cannot parse .p12: %w", err)
	}
	key, leaf, chain, err := pkcs12.DecodeChain(der, strings.TrimSpace(string(pass)))
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("sber tls: cannot read .p12 (wrong password?): %w", err)
	}
	cert := tls.Certificate{PrivateKey: key, Leaf: leaf, Certificate: [][]byte{leaf.Raw}}
	for _, c := range chain {
		cert.Certificate = append(cert.Certificate, c.Raw)
	}
	roots, err := x509.SystemCertPool()
	if err != nil || roots == nil {
		roots = x509.NewCertPool()
	}
	if f.CADir != "" {
		if err := addCAs(roots, f.CADir); err != nil {
			return nil, time.Time{}, err
		}
	}
	return &http.Client{
		Timeout: 60 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{Certificates: []tls.Certificate{cert}, RootCAs: roots, MinVersion: tls.VersionTLS12},
		},
	}, leaf.NotAfter, nil
}

// addCAs добавляет в пул все сертификаты каталога; PEM и DER допустимы, посторонние файлы пропускаются.
func addCAs(pool *x509.CertPool, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("sber ca dir: %w", err)
	}
	for _, e := range entries {
		ext := strings.ToLower(filepath.Ext(e.Name()))
		if e.IsDir() || (ext != ".cer" && ext != ".crt" && ext != ".pem") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return err
		}
		if block, _ := pem.Decode(data); block != nil {
			pool.AppendCertsFromPEM(data)
			continue
		}
		if c, err := x509.ParseCertificate(data); err == nil {
			pool.AddCert(c)
		}
	}
	return nil
}
