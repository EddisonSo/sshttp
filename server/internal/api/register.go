package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/eddison/sshttp/server/internal/store"
	"github.com/go-webauthn/webauthn/protocol"
)

type registerInfoResponse struct {
	Username string `json:"username"`
	IsNewUser bool   `json:"isNewUser"`
}

func (s *Server) handleRegisterInfo(w http.ResponseWriter, r *http.Request) {
	rid := r.URL.Query().Get("rid")
	if rid == "" {
		http.Error(w, "missing rid", http.StatusBadRequest)
		return
	}

	reg, err := s.store.GetRegistration(r.Context(), rid)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if reg == nil {
		http.Error(w, "invalid registration", http.StatusNotFound)
		return
	}
	if reg.Used {
		http.Error(w, "registration already used", http.StatusGone)
		return
	}
	if time.Now().After(reg.ExpiresAt) {
		http.Error(w, "registration expired", http.StatusGone)
		return
	}

	// Check if user exists
	existing, _ := s.store.GetUserByUsername(r.Context(), reg.Username)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registerInfoResponse{
		Username:  reg.Username,
		IsNewUser: existing == nil,
	})
}

type registerBeginRequest struct {
	RID      string `json:"rid"`
	Username string `json:"username"`
}

type registerBeginResponse struct {
	Options *protocol.CredentialCreation `json:"options"`
	State   string                       `json:"state"`
}

func (s *Server) handleRegisterBegin(w http.ResponseWriter, r *http.Request) {
	var req registerBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Validate registration
	reg, err := s.store.GetRegistration(r.Context(), req.RID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if reg == nil {
		http.Error(w, "invalid registration", http.StatusNotFound)
		return
	}
	if reg.Used {
		http.Error(w, "registration already used", http.StatusGone)
		return
	}
	if time.Now().After(reg.ExpiresAt) {
		http.Error(w, "registration expired", http.StatusGone)
		return
	}

	// Username must match the registration
	if req.Username != reg.Username {
		http.Error(w, "username mismatch", http.StatusBadRequest)
		return
	}

	// Check if user already exists
	user, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if user == nil {
		// Create new user
		user = &store.User{
			ID:        generateUserID(),
			Username:  req.Username,
			CreatedAt: time.Now(),
		}
		if err := s.store.CreateUser(r.Context(), user); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
	}
	// If user exists, we'll add a new credential to their account

	// Begin registration ceremony
	options, sessionID, err := s.webauthn.BeginRegistration(r.Context(), user)
	if err != nil {
		http.Error(w, "failed to begin registration", http.StatusInternalServerError)
		return
	}

	resp := registerBeginResponse{
		Options: options,
		State:   sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type registerFinishRequest struct {
	RID        string                                         `json:"rid"`
	State      string                                         `json:"state"`
	Credential *protocol.CredentialCreationResponse `json:"credential"`
}

type registerFinishResponse struct {
	Success bool `json:"success"`
}

func (s *Server) handleRegisterFinish(w http.ResponseWriter, r *http.Request) {
	var req registerFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Parse the credential creation response
	ccr, err := req.Credential.Parse()
	if err != nil {
		http.Error(w, "invalid credential", http.StatusBadRequest)
		return
	}

	// Finish registration
	credential, err := s.webauthn.FinishRegistration(r.Context(), req.State, ccr)
	if err != nil {
		http.Error(w, "registration failed: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Get registration to find user
	reg, err := s.store.GetRegistration(r.Context(), req.RID)
	if err != nil || reg == nil {
		http.Error(w, "invalid registration", http.StatusBadRequest)
		return
	}

	// Get user by username
	user, err := s.store.GetUserByUsername(r.Context(), reg.Username)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusInternalServerError)
		return
	}

	// Store credential
	cred := &store.Credential{
		ID:              credential.ID,
		UserID:          user.ID,
		PublicKey:       credential.PublicKey,
		AttestationType: credential.AttestationType,
		AAGUID:          credential.Authenticator.AAGUID,
		SignCount:       credential.Authenticator.SignCount,
		CreatedAt:       time.Now(),
	}

	if err := s.store.CreateCredential(r.Context(), cred); err != nil {
		http.Error(w, "failed to store credential", http.StatusInternalServerError)
		return
	}

	// Mark registration as used
	if err := s.store.MarkRegistrationUsed(r.Context(), req.RID); err != nil {
		// Log but don't fail
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(registerFinishResponse{Success: true})
}

func generateUserID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
