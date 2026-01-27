package store

import (
	"context"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

type User struct {
	ID          string
	Username    string
	DisplayName string
	CreatedAt   time.Time
}

// WebAuthnUser implements webauthn.User interface
func (u *User) WebAuthnID() []byte {
	return []byte(u.ID)
}

func (u *User) WebAuthnName() string {
	return u.Username
}

func (u *User) WebAuthnDisplayName() string {
	if u.DisplayName != "" {
		return u.DisplayName
	}
	return u.Username
}

func (u *User) WebAuthnIcon() string {
	return ""
}

func (u *User) WebAuthnCredentials() []webauthn.Credential {
	return nil // Credentials loaded separately
}

type Credential struct {
	ID              []byte
	UserID          string
	Name            string
	PublicKey       []byte
	AttestationType string
	AAGUID          []byte
	SignCount       uint32
	CreatedAt       time.Time
}

type Registration struct {
	ID        string
	Username  string
	CreatedAt time.Time
	ExpiresAt time.Time
	Used      bool
}

type Store interface {
	// User operations
	CreateUser(ctx context.Context, user *User) error
	GetUser(ctx context.Context, id string) (*User, error)
	GetUserByUsername(ctx context.Context, username string) (*User, error)

	// Credential operations
	CreateCredential(ctx context.Context, cred *Credential) error
	GetCredentialsByUserID(ctx context.Context, userID string) ([]Credential, error)
	GetCredentialByID(ctx context.Context, id []byte) (*Credential, error)
	UpdateCredentialSignCount(ctx context.Context, id []byte, signCount uint32) error
	UpdateCredentialName(ctx context.Context, id []byte, name string) error
	DeleteCredential(ctx context.Context, id []byte) error

	// Registration operations
	CreateRegistration(ctx context.Context, reg *Registration) error
	GetRegistration(ctx context.Context, id string) (*Registration, error)
	MarkRegistrationUsed(ctx context.Context, id string) error

	// Cleanup
	Close() error
}
