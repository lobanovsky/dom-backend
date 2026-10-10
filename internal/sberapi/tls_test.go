package sberapi

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"software.sslmate.com/src/go-pkcs12"
)

func TestNewHTTPClientReadsP12AndCAs(t *testing.T) {
	key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	tpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test-client"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(24 * time.Hour), IsCA: true, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tpl, tpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, _ := x509.ParseCertificate(der)
	p12, err := pkcs12.Modern.Encode(key, cert, nil, "pw")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	ca := filepath.Join(dir, "ca")
	os.Mkdir(ca, 0o700)
	os.WriteFile(filepath.Join(ca, "a.cer"), der, 0o600) // DER
	os.WriteFile(filepath.Join(ca, "b.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(filepath.Join(ca, "notes.txt"), []byte("ignored"), 0o600)

	c, expires, err := NewHTTPClient(TLSFiles{P12: write("t.p12", p12), PasswordFile: write("t.pass", []byte("pw\n")), CADir: ca})
	if err != nil || c == nil {
		t.Fatalf("err = %v", err)
	}
	if !expires.Equal(cert.NotAfter) {
		t.Errorf("expires = %v, want %v", expires, cert.NotAfter)
	}
	if _, _, err := NewHTTPClient(TLSFiles{P12: write("t.p12", p12), PasswordFile: write("bad.pass", []byte("nope"))}); err == nil {
		t.Error("wrong password must fail")
	}
}
