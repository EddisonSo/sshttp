// Package certreload serves a TLS certificate that is re-read from disk
// whenever the cert or key file changes, so renewals (e.g. by acme.sh) take
// effect without restarting the server and dropping shell sessions.
package certreload

import (
	"crypto/tls"
	"fmt"
	"log"
	"os"
	"sync"
	"time"
)

// Reloader holds the current certificate and the file mtimes it was loaded from.
type Reloader struct {
	certPath, keyPath string

	mu        sync.RWMutex
	cert      *tls.Certificate
	certMtime time.Time
	keyMtime  time.Time
}

// New loads the initial certificate. It fails if the files cannot be loaded.
func New(certPath, keyPath string) (*Reloader, error) {
	r := &Reloader{certPath: certPath, keyPath: keyPath}
	if err := r.reload(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Reloader) reload() error {
	certInfo, err := os.Stat(r.certPath)
	if err != nil {
		return err
	}
	keyInfo, err := os.Stat(r.keyPath)
	if err != nil {
		return err
	}
	cert, err := tls.LoadX509KeyPair(r.certPath, r.keyPath)
	if err != nil {
		return fmt.Errorf("load key pair: %w", err)
	}

	r.mu.Lock()
	r.cert = &cert
	r.certMtime = certInfo.ModTime()
	r.keyMtime = keyInfo.ModTime()
	r.mu.Unlock()

	if cert.Leaf != nil {
		log.Printf("loaded TLS certificate %s (expires %s)", cert.Leaf.Subject.CommonName, cert.Leaf.NotAfter.Format(time.RFC3339))
	}
	return nil
}

// changed reports whether either file's mtime differs from the loaded one.
func (r *Reloader) changed() bool {
	certInfo, err := os.Stat(r.certPath)
	if err != nil {
		return false
	}
	keyInfo, err := os.Stat(r.keyPath)
	if err != nil {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !certInfo.ModTime().Equal(r.certMtime) || !keyInfo.ModTime().Equal(r.keyMtime)
}

// GetCertificate implements tls.Config.GetCertificate. If the files changed
// since the last load it reloads them; on failure (e.g. a half-written file
// mid-renewal) it keeps serving the previous certificate and retries next time.
func (r *Reloader) GetCertificate(*tls.ClientHelloInfo) (*tls.Certificate, error) {
	if r.changed() {
		if err := r.reload(); err != nil {
			log.Printf("TLS certificate reload failed, keeping previous: %v", err)
		}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cert, nil
}
