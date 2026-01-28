package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	JWTSecret       string
	TokenExpiryMins int

	// Session
	SessionIdleTimeoutMins int
}

func Load() *Config {
	dataDir := getDefaultDataDir()

	// Ensure data directory exists
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		log.Fatalf("failed to create data directory: %v", err)
	}

	configPath := filepath.Join(dataDir, "config")
	cfg := loadOrCreateConfig(configPath, dataDir)

	return cfg
}

func loadOrCreateConfig(configPath, dataDir string) *Config {
	// Default values
	defaults := map[string]string{
		"addr":                      ":4422",
		"static_dir":                "",
		"tls_cert":                  filepath.Join(dataDir, "cert.pem"),
		"tls_key":                   filepath.Join(dataDir, "key.pem"),
		"rp_display_name":           "sshttp",
		"rp_id":                     "localhost",
		"rp_origin":                 "https://localhost:4422",
		"token_expiry_mins":         "15",
		"session_idle_timeout_mins": "30",
	}

	values := make(map[string]string)
	for k, v := range defaults {
		values[k] = v
	}

	// Try to load existing config
	if data, err := os.ReadFile(configPath); err == nil {
		parseConfig(string(data), values)
	} else if os.IsNotExist(err) {
		// Create default config file
		createDefaultConfig(configPath, defaults)
		log.Printf("Created default config at %s - please edit and restart", configPath)
	} else {
		log.Printf("Warning: could not read config file: %v", err)
	}

	return &Config{
		Addr:                   values["addr"],
		DataDir:                dataDir,
		StaticDir:              values["static_dir"],
		TLSCert:                values["tls_cert"],
		TLSKey:                 values["tls_key"],
		RPDisplayName:          values["rp_display_name"],
		RPID:                   values["rp_id"],
		RPOrigins:              []string{values["rp_origin"]},
		JWTSecret:              getOrCreateSecret(dataDir),
		TokenExpiryMins:        parseInt(values["token_expiry_mins"], 15),
		SessionIdleTimeoutMins: parseInt(values["session_idle_timeout_mins"], 30),
	}
}

func parseConfig(content string, values map[string]string) {
	scanner := bufio.NewScanner(strings.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Parse key = value
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		values[key] = value
	}
}

func createDefaultConfig(configPath string, defaults map[string]string) {
	content := `# sshttp configuration
# Edit this file to customize your server settings

# Server listen address (host:port)
addr = :4422

# Path to static frontend files (empty = use embedded)
static_dir =

# TLS certificate and key paths
tls_cert = ` + defaults["tls_cert"] + `
tls_key = ` + defaults["tls_key"] + `

# WebAuthn Relying Party settings
# IMPORTANT: Change these to match your domain
rp_display_name = sshttp
rp_id = localhost
rp_origin = https://localhost:4422

# JWT token expiry time in minutes
token_expiry_mins = 15

# Shell session idle timeout in minutes
session_idle_timeout_mins = 30
`

	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		log.Printf("Warning: could not create config file: %v", err)
	}
}

func getOrCreateSecret(dataDir string) string {
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

	// Save secret with restrictive permissions
	if err := os.WriteFile(secretFile, []byte(secretHex), 0600); err != nil {
		log.Printf("Warning: could not persist JWT secret: %v", err)
	}

	return secretHex
}

func parseInt(s string, defaultVal int) int {
	if i, err := strconv.Atoi(s); err == nil {
		return i
	}
	return defaultVal
}
