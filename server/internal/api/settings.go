package api

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"time"

	"github.com/eddison/sshttp/server/internal/middleware"
	"github.com/eddison/sshttp/server/internal/store"
	"github.com/go-webauthn/webauthn/protocol"
)

type keyInfo struct {
	ID                string    `json:"id"`
	Name              string    `json:"name"`
	AuthenticatorType string    `json:"authenticatorType"`
	CreatedAt         time.Time `json:"createdAt"`
}

type listKeysResponse struct {
	Keys []keyInfo `json:"keys"`
}

func (s *Server) handleListKeys(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	creds, err := s.store.GetCredentialsByUserID(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	keys := make([]keyInfo, len(creds))
	for i, c := range creds {
		keys[i] = keyInfo{
			ID:                base64.URLEncoding.EncodeToString(c.ID),
			Name:              c.Name,
			AuthenticatorType: s.mds.GetName(c.AAGUID),
			CreatedAt:         c.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(listKeysResponse{Keys: keys})
}

type deleteKeyRequest struct {
	ID string `json:"id"`
}

func (s *Server) handleDeleteKey(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req deleteKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Decode the key ID
	keyID, err := base64.URLEncoding.DecodeString(req.ID)
	if err != nil {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}

	// Get all user's credentials to ensure they have more than one
	creds, err := s.store.GetCredentialsByUserID(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	if len(creds) <= 1 {
		http.Error(w, "cannot delete last key", http.StatusBadRequest)
		return
	}

	// Verify the key belongs to the user
	found := false
	for _, c := range creds {
		if string(c.ID) == string(keyID) {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	// Delete the key
	if err := s.store.DeleteCredential(r.Context(), keyID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type addKeyBeginResponse struct {
	Options *protocol.CredentialCreation `json:"options"`
	State   string                       `json:"state"`
}

func (s *Server) handleAddKeyBegin(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Get user
	user, err := s.store.GetUser(r.Context(), claims.UserID)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusNotFound)
		return
	}

	// Begin registration ceremony for new key
	options, sessionID, err := s.webauthn.BeginRegistration(r.Context(), user)
	if err != nil {
		http.Error(w, "failed to begin registration", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(addKeyBeginResponse{
		Options: options,
		State:   sessionID,
	})
}

type addKeyFinishRequest struct {
	State      string                            `json:"state"`
	Credential *protocol.CredentialCreationResponse `json:"credential"`
}

func (s *Server) handleAddKeyFinish(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req addKeyFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Parse the credential
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

	// Store the new credential
	cred := &store.Credential{
		ID:              credential.ID,
		UserID:          claims.UserID,
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

	w.WriteHeader(http.StatusOK)
}

type renameKeyRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (s *Server) handleRenameKey(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req renameKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	// Decode the key ID
	keyID, err := base64.URLEncoding.DecodeString(req.ID)
	if err != nil {
		http.Error(w, "invalid key id", http.StatusBadRequest)
		return
	}

	// Get all user's credentials to verify ownership
	creds, err := s.store.GetCredentialsByUserID(r.Context(), claims.UserID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Verify the key belongs to the user
	found := false
	for _, c := range creds {
		if string(c.ID) == string(keyID) {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "key not found", http.StatusNotFound)
		return
	}

	// Update the key name
	if err := s.store.UpdateCredentialName(r.Context(), keyID, req.Name); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
