package api

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
)

type authBeginRequest struct {
	Username string `json:"username"`
}

type authBeginResponse struct {
	Options *protocol.CredentialAssertion `json:"options"`
	State   string                        `json:"state"`
}

func (s *Server) handleAuthBegin(w http.ResponseWriter, r *http.Request) {
	var req authBeginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Username == "" {
		http.Error(w, "username required", http.StatusBadRequest)
		return
	}

	// Get user
	user, err := s.store.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if user == nil {
		// Don't reveal if user exists
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	// Begin login ceremony
	options, sessionID, err := s.webauthn.BeginLogin(r.Context(), user)
	if err != nil {
		log.Printf("begin login error: %v", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	resp := authBeginResponse{
		Options: options,
		State:   sessionID,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type authFinishRequest struct {
	State      string                                        `json:"state"`
	Credential *protocol.CredentialAssertionResponse `json:"credential"`
}

type authFinishResponse struct {
	AccessToken string `json:"accessToken"`
}

func (s *Server) handleAuthFinish(w http.ResponseWriter, r *http.Request) {
	var req authFinishRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	// Parse the credential assertion response
	car, err := req.Credential.Parse()
	if err != nil {
		http.Error(w, "invalid credential", http.StatusBadRequest)
		return
	}

	// Finish login
	user, _, err := s.webauthn.FinishLogin(r.Context(), req.State, car)
	if err != nil {
		log.Printf("finish login error: %v", err)
		http.Error(w, "authentication failed", http.StatusUnauthorized)
		return
	}

	// Issue JWT token with IP binding
	clientIP := getClientIP(r)
	token, err := s.tokenManager.Issue(user.ID, user.Username, clientIP)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Log successful authentication
	log.Printf("user %s authenticated from %s", user.Username, clientIP)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(authFinishResponse{AccessToken: token})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Extract token from request
	token := r.Header.Get("Authorization")
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimPrefix(token, "Bearer ")
	} else {
		token = r.URL.Query().Get("token")
	}

	if token == "" {
		http.Error(w, "no token provided", http.StatusBadRequest)
		return
	}

	// Validate and get claims to extract token ID and expiry
	claims, err := s.tokenManager.Validate(token)
	if err != nil {
		// Token already invalid, consider logout successful
		w.WriteHeader(http.StatusOK)
		return
	}

	// Revoke the token
	if claims.ExpiresAt != nil {
		s.tokenManager.Revoke(claims.TokenID, claims.ExpiresAt.Time)
	}

	log.Printf("user %s logged out", claims.Username)
	w.WriteHeader(http.StatusOK)
}

// getClientIP extracts the real client IP from the request
func getClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (first IP is the client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0])
	}

	// Check X-Real-IP header
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Fall back to RemoteAddr
	// Strip port if present
	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		// Handle IPv6 addresses like [::1]:port
		if strings.Contains(addr, "]") {
			if bracketIdx := strings.LastIndex(addr, "]"); bracketIdx > idx {
				return addr // No port, return as-is
			}
		}
		return addr[:idx]
	}
	return addr
}
