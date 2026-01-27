package store

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(dataDir string) (*SQLiteStore, error) {
	if err := os.MkdirAll(dataDir, 0700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(dataDir, "sshttp.db")
	// Enable foreign keys and WAL mode for better concurrency and crash safety
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Verify WAL mode is enabled
	var journalMode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err == nil {
		if journalMode != "wal" {
			// Force WAL mode
			db.Exec("PRAGMA journal_mode=WAL")
		}
	}

	store := &SQLiteStore{db: db}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}

	return store, nil
}

func (s *SQLiteStore) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		display_name TEXT,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS credentials (
		id BLOB PRIMARY KEY,
		user_id TEXT NOT NULL REFERENCES users(id),
		name TEXT NOT NULL DEFAULT '',
		public_key BLOB NOT NULL,
		attestation_type TEXT NOT NULL,
		aaguid BLOB,
		sign_count INTEGER NOT NULL DEFAULT 0,
		created_at DATETIME NOT NULL
	);

	CREATE TABLE IF NOT EXISTS registrations (
		id TEXT PRIMARY KEY,
		username TEXT NOT NULL,
		created_at DATETIME NOT NULL,
		expires_at DATETIME NOT NULL,
		used INTEGER NOT NULL DEFAULT 0
	);

	CREATE INDEX IF NOT EXISTS idx_credentials_user_id ON credentials(user_id);
	CREATE INDEX IF NOT EXISTS idx_registrations_expires ON registrations(expires_at);
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return err
	}

	// Add name column to existing credentials table if it doesn't exist
	s.db.Exec("ALTER TABLE credentials ADD COLUMN name TEXT NOT NULL DEFAULT ''")

	return nil
}

func (s *SQLiteStore) CreateUser(ctx context.Context, user *User) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO users (id, username, display_name, created_at) VALUES (?, ?, ?, ?)",
		user.ID, user.Username, user.DisplayName, user.CreatedAt)
	return err
}

func (s *SQLiteStore) GetUser(ctx context.Context, id string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, username, display_name, created_at FROM users WHERE id = ?", id)

	var user User
	if err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *SQLiteStore) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, username, display_name, created_at FROM users WHERE username = ?", username)

	var user User
	if err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

func (s *SQLiteStore) CreateCredential(ctx context.Context, cred *Credential) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO credentials (id, user_id, name, public_key, attestation_type, aaguid, sign_count, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		cred.ID, cred.UserID, cred.Name, cred.PublicKey, cred.AttestationType, cred.AAGUID, cred.SignCount, cred.CreatedAt)
	return err
}

func (s *SQLiteStore) GetCredentialsByUserID(ctx context.Context, userID string) ([]Credential, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, user_id, name, public_key, attestation_type, aaguid, sign_count, created_at
		FROM credentials WHERE user_id = ?`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []Credential
	for rows.Next() {
		var c Credential
		if err := rows.Scan(&c.ID, &c.UserID, &c.Name, &c.PublicKey, &c.AttestationType, &c.AAGUID, &c.SignCount, &c.CreatedAt); err != nil {
			return nil, err
		}
		creds = append(creds, c)
	}
	return creds, rows.Err()
}

func (s *SQLiteStore) GetCredentialByID(ctx context.Context, id []byte) (*Credential, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id, user_id, name, public_key, attestation_type, aaguid, sign_count, created_at
		FROM credentials WHERE id = ?`, id)

	var c Credential
	if err := row.Scan(&c.ID, &c.UserID, &c.Name, &c.PublicKey, &c.AttestationType, &c.AAGUID, &c.SignCount, &c.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (s *SQLiteStore) UpdateCredentialSignCount(ctx context.Context, id []byte, signCount uint32) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE credentials SET sign_count = ? WHERE id = ?", signCount, id)
	return err
}

func (s *SQLiteStore) UpdateCredentialName(ctx context.Context, id []byte, name string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE credentials SET name = ? WHERE id = ?", name, id)
	return err
}

func (s *SQLiteStore) DeleteCredential(ctx context.Context, id []byte) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM credentials WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) CreateRegistration(ctx context.Context, reg *Registration) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO registrations (id, username, created_at, expires_at, used) VALUES (?, ?, ?, ?, ?)",
		reg.ID, reg.Username, reg.CreatedAt, reg.ExpiresAt, reg.Used)
	return err
}

func (s *SQLiteStore) GetRegistration(ctx context.Context, id string) (*Registration, error) {
	row := s.db.QueryRowContext(ctx,
		"SELECT id, username, created_at, expires_at, used FROM registrations WHERE id = ?", id)

	var reg Registration
	if err := row.Scan(&reg.ID, &reg.Username, &reg.CreatedAt, &reg.ExpiresAt, &reg.Used); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &reg, nil
}

func (s *SQLiteStore) MarkRegistrationUsed(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx,
		"UPDATE registrations SET used = 1 WHERE id = ?", id)
	return err
}

func (s *SQLiteStore) Close() error {
	return s.db.Close()
}

// Cleanup expired registrations
func (s *SQLiteStore) CleanupExpiredRegistrations(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		"DELETE FROM registrations WHERE expires_at < ?", time.Now())
	return err
}
