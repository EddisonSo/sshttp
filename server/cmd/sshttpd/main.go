package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"syscall"
	"time"

	"github.com/eddison/sshttp/server/internal/api"
	"github.com/eddison/sshttp/server/internal/auth"
	"github.com/eddison/sshttp/server/internal/config"
	"github.com/eddison/sshttp/server/internal/mds"
	"github.com/eddison/sshttp/server/internal/pty"
	"github.com/eddison/sshttp/server/internal/store"
)

func main() {
	register := flag.Bool("register", false, "Generate one-time registration link for current user")
	flag.Parse()

	cfg := config.Load()

	// Initialize store
	s, err := store.NewSQLiteStore(cfg.DataDir)
	if err != nil {
		log.Fatalf("failed to initialize store: %v", err)
	}
	defer s.Close()

	// Handle registration command
	if *register {
		// Get current Unix user
		currentUser, err := user.Current()
		if err != nil {
			log.Fatalf("failed to get current user: %v", err)
		}
		if err := generateRegistration(s, currentUser.Username, cfg); err != nil {
			log.Fatalf("failed to create registration: %v", err)
		}
		return
	}

	// Initialize WebAuthn handler
	wa, err := auth.NewWebAuthnHandler(cfg, s)
	if err != nil {
		log.Fatalf("failed to initialize webauthn: %v", err)
	}

	// Initialize token manager
	tm := auth.NewTokenManager(cfg.JWTSecret, cfg.TokenExpiryMins)

	// Initialize session manager
	sm := pty.NewSessionManager()

	// Initialize MDS client for authenticator metadata
	mdsClient := mds.New(cfg.DataDir)
	mdsClient.Load()

	// Create server
	srv := api.NewServer(cfg, s, wa, tm, sm, mdsClient)

	// Set embedded filesystem if available
	srv.SetEmbeddedFS(StaticFS)
	log.Println("using embedded static files")

	// Start HTTP server
	httpServer := &http.Server{
		Addr:    cfg.Addr,
		Handler: srv.Router(),
	}

	// Graceful shutdown
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		<-sigCh

		log.Println("shutting down...")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		httpServer.Shutdown(ctx)
	}()

	// Start cleanup goroutines
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			wa.CleanupExpiredSessions()
			sm.CloseIdleSessions(time.Duration(cfg.SessionIdleTimeoutMins) * time.Minute)
		}
	}()

	// Start server (TLS if configured)
	if cfg.TLSCert != "" && cfg.TLSKey != "" {
		log.Printf("starting HTTPS server on %s", cfg.Addr)
		if err := httpServer.ListenAndServeTLS(cfg.TLSCert, cfg.TLSKey); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	} else {
		log.Printf("starting HTTP server on %s (WebAuthn requires HTTPS!)", cfg.Addr)
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}
}

func generateRegistration(s *store.SQLiteStore, username string, cfg *config.Config) error {
	// Check if user already exists
	existing, err := s.GetUserByUsername(context.Background(), username)
	if err != nil {
		return err
	}

	// Generate secure registration ID
	rid, err := secureRandomString(32)
	if err != nil {
		return fmt.Errorf("generate registration ID: %w", err)
	}

	reg := &store.Registration{
		ID:        rid,
		Username:  username,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
		Used:      false,
	}

	if err := s.CreateRegistration(context.Background(), reg); err != nil {
		return err
	}

	// Print registration URL
	origin := cfg.RPOrigins[0]
	if existing != nil {
		fmt.Printf("\nAdd new passkey for %s:\n", username)
	} else {
		fmt.Printf("\nRegistration link for %s:\n", username)
	}
	fmt.Printf("%s/register?rid=%s\n\n", origin, rid)
	fmt.Printf("This link expires in 24 hours.\n")

	return nil
}

func secureRandomString(n int) (string, error) {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}
