package api

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eddison/sshttp/server/internal/middleware"
)

type fileEntry struct {
	Name    string    `json:"name"`
	IsDir   bool      `json:"isDir"`
	Size    int64     `json:"size"`
	ModTime time.Time `json:"modTime"`
}

type listFilesResponse struct {
	Path    string      `json:"path"`
	Entries []fileEntry `json:"entries"`
}

// resolveSessionPath returns an absolute, cleaned path scoped to a session
// owned by userID. If path is empty, the session's current working directory
// is returned. Relative paths are resolved against the session cwd.
func (s *Server) resolveSessionPath(sessionID, userID, path string) (string, error) {
	session, ok := s.sessionManager.Get(sessionID)
	if !ok || session.UserID != userID {
		return "", os.ErrNotExist
	}

	cwd, err := session.GetWorkingDir()
	if err != nil {
		return "", err
	}

	if path == "" {
		return cwd, nil
	}
	if strings.ContainsRune(path, 0) {
		return "", os.ErrInvalid
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(cwd, path)
	}
	return filepath.Clean(path), nil
}

func (s *Server) handleListFiles(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "sessionId required", http.StatusBadRequest)
		return
	}

	dir, err := s.resolveSessionPath(sessionID, claims.UserID, r.URL.Query().Get("path"))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "directory not found", http.StatusNotFound)
			return
		}
		if os.IsPermission(err) {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		http.Error(w, "failed to read directory", http.StatusInternalServerError)
		return
	}

	entries := make([]fileEntry, 0, len(dirEntries))
	for _, de := range dirEntries {
		full := filepath.Join(dir, de.Name())
		info, err := os.Stat(full)
		if err != nil {
			continue
		}
		entries = append(entries, fileEntry{
			Name:    de.Name(),
			IsDir:   info.IsDir(),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(listFilesResponse{Path: dir, Entries: entries})
}

func (s *Server) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		http.Error(w, "sessionId required", http.StatusBadRequest)
		return
	}

	path, err := s.resolveSessionPath(sessionID, claims.UserID, r.URL.Query().Get("path"))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "session not found", http.StatusNotFound)
			return
		}
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "file not found", http.StatusNotFound)
			return
		}
		if os.IsPermission(err) {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		http.Error(w, "failed to stat file", http.StatusInternalServerError)
		return
	}
	if !info.Mode().IsRegular() {
		http.Error(w, "not a regular file", http.StatusBadRequest)
		return
	}

	f, err := os.Open(path)
	if err != nil {
		if os.IsPermission(err) {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		http.Error(w, "failed to open file", http.StatusInternalServerError)
		return
	}
	defer f.Close()

	name := filepath.Base(path)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", contentDispositionAttachment(name))
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// contentDispositionAttachment builds a Content-Disposition header that handles
// non-ASCII filenames per RFC 5987.
func contentDispositionAttachment(name string) string {
	var ascii strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			ascii.WriteByte('_')
		} else {
			ascii.WriteRune(r)
		}
	}
	return "attachment; filename=\"" + ascii.String() + "\"; filename*=UTF-8''" + urlPathEscape(name)
}

// urlPathEscape percent-encodes a UTF-8 string for use in filename* per RFC 5987.
func urlPathEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		if (c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			c == '!' || c == '#' || c == '$' || c == '&' || c == '+' || c == '-' ||
			c == '.' || c == '^' || c == '_' || c == '`' || c == '|' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}
