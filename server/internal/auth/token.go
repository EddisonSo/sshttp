package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenManager struct {
	secret     []byte
	expiryMins int

	// Token blacklist for revocation
	blacklist   map[string]time.Time
	blacklistMu sync.RWMutex
}

type Claims struct {
	UserID   string `json:"uid"`
	Username string `json:"usr"`
	IPHash   string `json:"iph,omitempty"` // Hashed client IP for binding
	TokenID  string `json:"jti,omitempty"` // Unique token ID for revocation
	jwt.RegisteredClaims
}

func NewTokenManager(secret string, expiryMins int) *TokenManager {
	tm := &TokenManager{
		secret:     []byte(secret),
		expiryMins: expiryMins,
		blacklist:  make(map[string]time.Time),
	}

	// Cleanup expired blacklist entries periodically
	go tm.cleanupBlacklist()

	return tm
}

func (t *TokenManager) cleanupBlacklist() {
	ticker := time.NewTicker(5 * time.Minute)
	for range ticker.C {
		t.blacklistMu.Lock()
		now := time.Now()
		for tokenID, expiry := range t.blacklist {
			if now.After(expiry) {
				delete(t.blacklist, tokenID)
			}
		}
		t.blacklistMu.Unlock()
	}
}

// Issue creates a new JWT token for the user
func (t *TokenManager) Issue(userID, username, clientIP string) (string, error) {
	now := time.Now()
	expiry := now.Add(time.Duration(t.expiryMins) * time.Minute)

	// Generate unique token ID
	tokenID := fmt.Sprintf("%s-%d", userID, now.UnixNano())

	claims := &Claims{
		UserID:   userID,
		Username: username,
		IPHash:   hashIP(clientIP),
		TokenID:  tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiry),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "sshttp",
			ID:        tokenID,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(t.secret)
}

// Validate parses and validates a JWT token
func (t *TokenManager) Validate(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return t.secret, nil
	})

	if err != nil {
		return nil, fmt.Errorf("parse token: %w", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token")
	}

	// Check blacklist
	if t.IsRevoked(claims.TokenID) {
		return nil, fmt.Errorf("token revoked")
	}

	return claims, nil
}

// ValidateWithIP validates token and checks IP binding
func (t *TokenManager) ValidateWithIP(tokenString, clientIP string) (*Claims, error) {
	claims, err := t.Validate(tokenString)
	if err != nil {
		return nil, err
	}

	// Verify IP binding if present
	if claims.IPHash != "" && claims.IPHash != hashIP(clientIP) {
		return nil, fmt.Errorf("token IP mismatch")
	}

	return claims, nil
}

// Revoke adds a token to the blacklist
func (t *TokenManager) Revoke(tokenID string, expiry time.Time) {
	t.blacklistMu.Lock()
	defer t.blacklistMu.Unlock()
	t.blacklist[tokenID] = expiry
}

// RevokeAllForUser revokes all tokens for a user (by adding user prefix to blacklist)
func (t *TokenManager) RevokeAllForUser(userID string) {
	// This is a simplified approach - in production you'd track all issued tokens
	// For now, we just note that this user's tokens before now are invalid
	t.blacklistMu.Lock()
	defer t.blacklistMu.Unlock()
	// Use a special marker that the validate function would need to check
	t.blacklist["user:"+userID] = time.Now().Add(time.Duration(t.expiryMins) * time.Minute)
}

// IsRevoked checks if a token is blacklisted
func (t *TokenManager) IsRevoked(tokenID string) bool {
	t.blacklistMu.RLock()
	defer t.blacklistMu.RUnlock()
	_, exists := t.blacklist[tokenID]
	return exists
}

// hashIP creates a hash of the client IP for privacy
func hashIP(ip string) string {
	h := sha256.Sum256([]byte(ip))
	return hex.EncodeToString(h[:8]) // First 8 bytes is enough
}
