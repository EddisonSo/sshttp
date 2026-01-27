package auth

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/eddison/sshttp/server/internal/config"
	"github.com/eddison/sshttp/server/internal/store"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

type WebAuthnHandler struct {
	webauthn *webauthn.WebAuthn
	store    store.Store

	// Session storage for registration/authentication ceremonies
	sessions sync.Map
}

type sessionData struct {
	data      *webauthn.SessionData
	userID    string
	username  string
	expiresAt time.Time
}

func NewWebAuthnHandler(cfg *config.Config, s store.Store) (*WebAuthnHandler, error) {
	wconfig := &webauthn.Config{
		RPDisplayName:         cfg.RPDisplayName,
		RPID:                  cfg.RPID,
		RPOrigins:             cfg.RPOrigins,
		AttestationPreference: protocol.PreferDirectAttestation,
	}

	w, err := webauthn.New(wconfig)
	if err != nil {
		return nil, fmt.Errorf("create webauthn: %w", err)
	}

	return &WebAuthnHandler{
		webauthn: w,
		store:    s,
	}, nil
}

// userWithCredentials wraps a store.User with loaded credentials
type userWithCredentials struct {
	*store.User
	credentials []webauthn.Credential
}

func (u *userWithCredentials) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

func (h *WebAuthnHandler) loadUserWithCredentials(ctx context.Context, user *store.User) (*userWithCredentials, error) {
	creds, err := h.store.GetCredentialsByUserID(ctx, user.ID)
	if err != nil {
		return nil, err
	}

	webCreds := make([]webauthn.Credential, len(creds))
	for i, c := range creds {
		webCreds[i] = webauthn.Credential{
			ID:              c.ID,
			PublicKey:       c.PublicKey,
			AttestationType: c.AttestationType,
			Authenticator: webauthn.Authenticator{
				AAGUID:    c.AAGUID,
				SignCount: c.SignCount,
			},
		}
	}

	return &userWithCredentials{
		User:        user,
		credentials: webCreds,
	}, nil
}

// BeginRegistration starts a WebAuthn registration ceremony
func (h *WebAuthnHandler) BeginRegistration(ctx context.Context, user *store.User) (*protocol.CredentialCreation, string, error) {
	options, session, err := h.webauthn.BeginRegistration(user)
	if err != nil {
		return nil, "", err
	}

	sessionID := generateSessionID()
	h.sessions.Store(sessionID, &sessionData{
		data:      session,
		userID:    user.ID,
		username:  user.Username,
		expiresAt: time.Now().Add(5 * time.Minute),
	})

	return options, sessionID, nil
}

// FinishRegistration completes the registration ceremony
func (h *WebAuthnHandler) FinishRegistration(ctx context.Context, sessionID string, response *protocol.ParsedCredentialCreationData) (*webauthn.Credential, error) {
	val, ok := h.sessions.LoadAndDelete(sessionID)
	if !ok {
		return nil, fmt.Errorf("session not found")
	}

	session := val.(*sessionData)
	if time.Now().After(session.expiresAt) {
		return nil, fmt.Errorf("session expired")
	}

	user, err := h.store.GetUser(ctx, session.userID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, fmt.Errorf("user not found")
	}

	credential, err := h.webauthn.CreateCredential(user, *session.data, response)
	if err != nil {
		return nil, err
	}

	return credential, nil
}

// BeginLogin starts a WebAuthn authentication ceremony
func (h *WebAuthnHandler) BeginLogin(ctx context.Context, user *store.User) (*protocol.CredentialAssertion, string, error) {
	userWithCreds, err := h.loadUserWithCredentials(ctx, user)
	if err != nil {
		return nil, "", err
	}

	options, session, err := h.webauthn.BeginLogin(userWithCreds)
	if err != nil {
		return nil, "", err
	}

	sessionID := generateSessionID()
	h.sessions.Store(sessionID, &sessionData{
		data:      session,
		userID:    user.ID,
		username:  user.Username,
		expiresAt: time.Now().Add(5 * time.Minute),
	})

	return options, sessionID, nil
}

// FinishLogin completes the authentication ceremony
func (h *WebAuthnHandler) FinishLogin(ctx context.Context, sessionID string, response *protocol.ParsedCredentialAssertionData) (*store.User, *webauthn.Credential, error) {
	val, ok := h.sessions.LoadAndDelete(sessionID)
	if !ok {
		return nil, nil, fmt.Errorf("session not found")
	}

	session := val.(*sessionData)
	if time.Now().After(session.expiresAt) {
		return nil, nil, fmt.Errorf("session expired")
	}

	user, err := h.store.GetUser(ctx, session.userID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil {
		return nil, nil, fmt.Errorf("user not found")
	}

	userWithCreds, err := h.loadUserWithCredentials(ctx, user)
	if err != nil {
		return nil, nil, err
	}

	credential, err := h.webauthn.ValidateLogin(userWithCreds, *session.data, response)
	if err != nil {
		return nil, nil, err
	}

	// Update sign count
	if err := h.store.UpdateCredentialSignCount(ctx, credential.ID, credential.Authenticator.SignCount); err != nil {
		// Log but don't fail
	}

	return user, credential, nil
}

// CleanupExpiredSessions removes expired sessions
func (h *WebAuthnHandler) CleanupExpiredSessions() {
	h.sessions.Range(func(key, value any) bool {
		session := value.(*sessionData)
		if time.Now().After(session.expiresAt) {
			h.sessions.Delete(key)
		}
		return true
	})
}

func generateSessionID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixNano(), time.Now().UnixNano()%1000000)
}
