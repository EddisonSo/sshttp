package config

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strconv"
)

func getDefaultDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/var/lib/sshttp"
	}
	return filepath.Join(home, ".sshttp")
}

type Config struct {
	// Server
	Addr      string
	DataDir   string
	StaticDir string
	TLSCert   string
	TLSKey    string

	// WebAuthn
	RPDisplayName string
	RPID          string
	RPOrigins     []string

	// JWT
	JWTSecret        string
	TokenExpiryMins  int

	// Session
	SessionIdleTimeoutMins int
}

func Load() *Config {
	dataDir := getEnv("SSHTTP_DATA_DIR", getDefaultDataDir())

	return &Config{
		Addr:                   getEnv("SSHTTP_ADDR", ":4422"),
		DataDir:                dataDir,
		StaticDir:              getEnv("SSHTTP_STATIC_DIR", ""), // Empty = use embedded
		TLSCert:                getEnv("SSHTTP_TLS_CERT", filepath.Join(dataDir, "cert.pem")),
		TLSKey:                 getEnv("SSHTTP_TLS_KEY", filepath.Join(dataDir, "key.pem")),
		RPDisplayName:          getEnv("SSHTTP_RP_DISPLAY_NAME", "sshttp"),
		RPID:                   getEnv("SSHTTP_RP_ID", "ssh.eddisonso.com"),
		RPOrigins:              []string{getEnv("SSHTTP_RP_ORIGIN", "https://ssh.eddisonso.com:4422")},
		JWTSecret:              getOrCreateSecret(dataDir, "SSHTTP_JWT_SECRET"),
		TokenExpiryMins:        getEnvInt("SSHTTP_TOKEN_EXPIRY_MINS", 15),
		SessionIdleTimeoutMins: getEnvInt("SSHTTP_SESSION_IDLE_TIMEOUT_MINS", 30),
	}
}

// getOrCreateSecret returns env var if set, otherwise loads/creates a persistent secret
func getOrCreateSecret(dataDir, envKey string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}

	secretFile := filepath.Join(dataDir, ".jwt_secret")

	// Try to read existing secret
	if data, err := os.ReadFile(secretFile); err == nil && len(data) >= 32 {
		return string(data)
	}

	// Generate new secret
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		log.Fatalf("failed to generate JWT secret: %v", err)
	}
	secretHex := hex.EncodeToString(secret)

	// Ensure data dir exists
	os.MkdirAll(dataDir, 0700)

	// Save secret with restrictive permissions
	if err := os.WriteFile(secretFile, []byte(secretHex), 0600); err != nil {
		log.Printf("warning: could not persist JWT secret: %v", err)
	}

	return secretHex
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}
