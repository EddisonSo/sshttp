package certreload

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
)

func writeCert(t *testing.T, certPath, keyPath, cn string, mtime time.Time) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600)
	os.Chtimes(certPath, mtime, mtime)
	os.Chtimes(keyPath, mtime, mtime)
}

func TestReloadsOnChange(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	base := time.Now().Add(-time.Hour)
	writeCert(t, certPath, keyPath, "old", base)

	r, err := New(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := r.GetCertificate(nil); c.Leaf.Subject.CommonName != "old" {
		t.Fatalf("got %q, want old", c.Leaf.Subject.CommonName)
	}

	writeCert(t, certPath, keyPath, "new", base.Add(time.Minute))
	if c, _ := r.GetCertificate(nil); c.Leaf.Subject.CommonName != "new" {
		t.Fatalf("got %q, want new", c.Leaf.Subject.CommonName)
	}
}

func TestKeepsPreviousOnBadReload(t *testing.T) {
	dir := t.TempDir()
	certPath, keyPath := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	writeCert(t, certPath, keyPath, "good", time.Now().Add(-time.Hour))

	r, err := New(certPath, keyPath)
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(certPath, []byte("garbage"), 0o600)

	c, err := r.GetCertificate(nil)
	if err != nil || c.Leaf.Subject.CommonName != "good" {
		t.Fatalf("expected previous cert to be kept, got %v / %v", c, err)
	}
}
