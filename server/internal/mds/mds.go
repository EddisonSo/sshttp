package mds

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Client fetches and caches FIDO Metadata Service data
type Client struct {
	mu      sync.RWMutex
	aaguids map[string]string // AAGUID -> description
	dataDir string
}

type mdsPayload struct {
	Entries []mdsEntry `json:"entries"`
}

type mdsEntry struct {
	AAGUID            string       `json:"aaguid"`
	MetadataStatement *mdsMetadata `json:"metadataStatement"`
}

type mdsMetadata struct {
	Description string `json:"description"`
}

func New(dataDir string) *Client {
	return &Client{
		aaguids: make(map[string]string),
		dataDir: dataDir,
	}
}

// Load fetches MDS data (call once at startup)
func (c *Client) Load() {
	cacheFile := filepath.Join(c.dataDir, "mds.json")

	// Try to load from cache first
	if c.loadFromCache(cacheFile) {
		return
	}

	// Fetch from network
	c.fetchAndCache(cacheFile)
}

func (c *Client) loadFromCache(cacheFile string) bool {
	info, err := os.Stat(cacheFile)
	if err != nil {
		return false
	}

	// Cache valid for 7 days
	if time.Since(info.ModTime()) > 7*24*time.Hour {
		return false
	}

	data, err := os.ReadFile(cacheFile)
	if err != nil {
		return false
	}

	return c.parsePayload(data)
}

func (c *Client) fetchAndCache(cacheFile string) {
	resp, err := http.Get("https://mds.fidoalliance.org/")
	if err != nil {
		log.Printf("failed to fetch MDS: %v", err)
		return
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("failed to read MDS response: %v", err)
		return
	}

	if resp.StatusCode != 200 {
		preview := string(body)
		if len(preview) > 100 {
			preview = preview[:100]
		}
		log.Printf("MDS returned status %d: %s", resp.StatusCode, preview)
		return
	}

	// Parse JWT - extract payload (second part)
	parts := strings.Split(string(body), ".")
	if len(parts) != 3 {
		log.Printf("invalid MDS JWT format")
		return
	}

	decoded, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		log.Printf("failed to decode MDS payload: %v", err)
		return
	}

	// Cache the decoded payload
	os.WriteFile(cacheFile, decoded, 0644)

	c.parsePayload(decoded)
}

func (c *Client) parsePayload(data []byte) bool {
	var mds mdsPayload
	if err := json.Unmarshal(data, &mds); err != nil {
		log.Printf("failed to parse MDS JSON: %v", err)
		return false
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for _, entry := range mds.Entries {
		if entry.AAGUID != "" && entry.MetadataStatement != nil && entry.MetadataStatement.Description != "" {
			aaguid := normalizeAAGUID(entry.AAGUID)
			c.aaguids[aaguid] = entry.MetadataStatement.Description
		}
	}

	log.Printf("loaded %d authenticators from FIDO MDS", len(c.aaguids))
	return true
}

// GetName returns the authenticator name for an AAGUID
func (c *Client) GetName(aaguid []byte) string {
	uuid := formatAAGUID(aaguid)

	c.mu.RLock()
	defer c.mu.RUnlock()

	if name, ok := c.aaguids[uuid]; ok {
		return name
	}

	// Check if all zeros
	for _, b := range aaguid {
		if b != 0 {
			return "Passkey"
		}
	}
	return "Passkey"
}

func normalizeAAGUID(s string) string {
	s = strings.ToLower(s)
	if !strings.Contains(s, "-") && len(s) == 32 {
		s = s[0:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:32]
	}
	return s
}

func formatAAGUID(aaguid []byte) string {
	if len(aaguid) != 16 {
		return ""
	}
	const h = "0123456789abcdef"
	b := aaguid
	return string([]byte{
		h[b[0]>>4], h[b[0]&0xf], h[b[1]>>4], h[b[1]&0xf],
		h[b[2]>>4], h[b[2]&0xf], h[b[3]>>4], h[b[3]&0xf], '-',
		h[b[4]>>4], h[b[4]&0xf], h[b[5]>>4], h[b[5]&0xf], '-',
		h[b[6]>>4], h[b[6]&0xf], h[b[7]>>4], h[b[7]&0xf], '-',
		h[b[8]>>4], h[b[8]&0xf], h[b[9]>>4], h[b[9]&0xf], '-',
		h[b[10]>>4], h[b[10]&0xf], h[b[11]>>4], h[b[11]&0xf],
		h[b[12]>>4], h[b[12]&0xf], h[b[13]>>4], h[b[13]&0xf],
		h[b[14]>>4], h[b[14]&0xf], h[b[15]>>4], h[b[15]&0xf],
	})
}
