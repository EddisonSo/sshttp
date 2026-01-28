package api

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/eddison/sshttp/server/internal/middleware"
)

var safeNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_\- ]+$`)

func (s *Server) getColorsDir() string {
	return filepath.Join(s.cfg.DataDir, "colors")
}

func (s *Server) getFontsDir() string {
	return filepath.Join(s.cfg.DataDir, "fonts")
}

func (s *Server) getPrefsFile() string {
	return filepath.Join(s.cfg.DataDir, "prefs.json")
}

// Preferences stored in prefs.json
type userPrefs struct {
	ActiveTheme string `json:"activeTheme"`
	ActiveFont  string `json:"activeFont"`
}

func (s *Server) loadPrefs() (*userPrefs, error) {
	data, err := os.ReadFile(s.getPrefsFile())
	if err != nil {
		if os.IsNotExist(err) {
			return &userPrefs{ActiveTheme: "Default", ActiveFont: "AdwaitaMono NF"}, nil
		}
		return nil, err
	}
	var prefs userPrefs
	if err := json.Unmarshal(data, &prefs); err != nil {
		return &userPrefs{ActiveTheme: "Default", ActiveFont: "AdwaitaMono NF"}, nil
	}
	return &prefs, nil
}

func (s *Server) savePrefs(prefs *userPrefs) error {
	data, err := json.MarshalIndent(prefs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.getPrefsFile(), data, 0644)
}

// Theme handlers

type themeInfo struct {
	Name string `json:"name"`
}

type listThemesResponse struct {
	Themes      []themeInfo `json:"themes"`
	ActiveTheme string      `json:"activeTheme"`
}

func (s *Server) handleListThemes(w http.ResponseWriter, r *http.Request) {
	colorsDir := s.getColorsDir()

	// Ensure directory exists
	if err := os.MkdirAll(colorsDir, 0755); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	entries, err := os.ReadDir(colorsDir)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	themes := []themeInfo{}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".itermcolors") {
			name := strings.TrimSuffix(entry.Name(), ".itermcolors")
			themes = append(themes, themeInfo{Name: name})
		}
	}

	prefs, _ := s.loadPrefs()
	activeTheme := "Default"
	if prefs != nil {
		activeTheme = prefs.ActiveTheme
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(listThemesResponse{
		Themes:      themes,
		ActiveTheme: activeTheme,
	})
}

func (s *Server) handleGetTheme(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" || !safeNameRegex.MatchString(name) {
		http.Error(w, "invalid theme name", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(s.getColorsDir(), name+".itermcolors")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "theme not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	w.Header().Set("Content-Type", "application/xml")
	w.Write(data)
}

type saveThemeRequest struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

func (s *Server) handleSaveTheme(w http.ResponseWriter, r *http.Request) {
	var req saveThemeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Name == "" || !safeNameRegex.MatchString(req.Name) {
		http.Error(w, "invalid theme name", http.StatusBadRequest)
		return
	}

	if req.Content == "" {
		http.Error(w, "content is required", http.StatusBadRequest)
		return
	}

	colorsDir := s.getColorsDir()
	if err := os.MkdirAll(colorsDir, 0755); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	filePath := filepath.Join(colorsDir, req.Name+".itermcolors")
	if err := os.WriteFile(filePath, []byte(req.Content), 0644); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

type deleteThemeRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleDeleteTheme(w http.ResponseWriter, r *http.Request) {
	var req deleteThemeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Name == "" || !safeNameRegex.MatchString(req.Name) {
		http.Error(w, "invalid theme name", http.StatusBadRequest)
		return
	}

	filePath := filepath.Join(s.getColorsDir(), req.Name+".itermcolors")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "theme not found", http.StatusNotFound)
		} else {
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	// If this was the active theme, reset to default
	prefs, _ := s.loadPrefs()
	if prefs != nil && prefs.ActiveTheme == req.Name {
		prefs.ActiveTheme = "Default"
		s.savePrefs(prefs)
	}

	w.WriteHeader(http.StatusOK)
}

type setActiveThemeRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleSetActiveTheme(w http.ResponseWriter, r *http.Request) {
	var req setActiveThemeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	prefs, err := s.loadPrefs()
	if err != nil {
		prefs = &userPrefs{ActiveFont: "AdwaitaMono NF"}
	}
	prefs.ActiveTheme = req.Name

	if err := s.savePrefs(prefs); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// Font handlers

type fontInfo struct {
	Name string `json:"name"`
	Ext  string `json:"ext"` // File extension (ttf, otf, woff, woff2)
}

type listFontsResponse struct {
	Fonts      []fontInfo `json:"fonts"`
	ActiveFont string     `json:"activeFont"`
}

func (s *Server) handleListFonts(w http.ResponseWriter, r *http.Request) {
	fontsDir := s.getFontsDir()

	// Ensure directory exists
	if err := os.MkdirAll(fontsDir, 0755); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	entries, err := os.ReadDir(fontsDir)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	fonts := []fontInfo{}
	validExts := map[string]bool{".ttf": true, ".otf": true, ".woff": true, ".woff2": true}
	for _, entry := range entries {
		if !entry.IsDir() {
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if validExts[ext] {
				name := strings.TrimSuffix(entry.Name(), ext)
				fonts = append(fonts, fontInfo{Name: name, Ext: ext[1:]}) // Remove leading dot
			}
		}
	}

	prefs, _ := s.loadPrefs()
	activeFont := "AdwaitaMono NF"
	if prefs != nil {
		activeFont = prefs.ActiveFont
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(listFontsResponse{
		Fonts:      fonts,
		ActiveFont: activeFont,
	})
}

func (s *Server) handleGetFont(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" || !safeNameRegex.MatchString(name) {
		http.Error(w, "invalid font name", http.StatusBadRequest)
		return
	}

	s.serveFont(w, name)
}

// handlePublicFont serves font files without authentication (for CSS @font-face)
func (s *Server) handlePublicFont(w http.ResponseWriter, r *http.Request) {
	// Extract font name from path: /fonts/{name}.{ext}
	path := strings.TrimPrefix(r.URL.Path, "/fonts/")
	if path == "" {
		http.Error(w, "font name required", http.StatusBadRequest)
		return
	}

	// Remove extension to get the name
	ext := filepath.Ext(path)
	name := strings.TrimSuffix(path, ext)

	if name == "" || !safeNameRegex.MatchString(name) {
		http.Error(w, "invalid font name", http.StatusBadRequest)
		return
	}

	s.serveFont(w, name)
}

func (s *Server) serveFont(w http.ResponseWriter, name string) {
	fontsDir := s.getFontsDir()
	validExts := []string{".ttf", ".otf", ".woff", ".woff2"}

	var filePath string
	var contentType string
	for _, ext := range validExts {
		path := filepath.Join(fontsDir, name+ext)
		if _, err := os.Stat(path); err == nil {
			filePath = path
			switch ext {
			case ".ttf":
				contentType = "font/ttf"
			case ".otf":
				contentType = "font/otf"
			case ".woff":
				contentType = "font/woff"
			case ".woff2":
				contentType = "font/woff2"
			}
			break
		}
	}

	if filePath == "" {
		http.Error(w, "font not found", http.StatusNotFound)
		return
	}

	data, err := os.ReadFile(filePath)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Allow caching for fonts
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	w.Header().Set("Content-Type", contentType)
	w.Write(data)
}

func (s *Server) handleUploadFont(w http.ResponseWriter, r *http.Request) {
	// Limit upload size to 10MB
	r.ParseMultipartForm(10 << 20)

	file, header, err := r.FormFile("font")
	if err != nil {
		http.Error(w, "invalid file", http.StatusBadRequest)
		return
	}
	defer file.Close()

	// Validate extension
	ext := strings.ToLower(filepath.Ext(header.Filename))
	validExts := map[string]bool{".ttf": true, ".otf": true, ".woff": true, ".woff2": true}
	if !validExts[ext] {
		http.Error(w, "invalid file type", http.StatusBadRequest)
		return
	}

	// Get font name from filename
	name := strings.TrimSuffix(header.Filename, ext)
	if !safeNameRegex.MatchString(name) {
		http.Error(w, "invalid font name", http.StatusBadRequest)
		return
	}

	fontsDir := s.getFontsDir()
	if err := os.MkdirAll(fontsDir, 0755); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Read file content
	data, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "failed to read file", http.StatusInternalServerError)
		return
	}

	// Save file
	filePath := filepath.Join(fontsDir, header.Filename)
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(fontInfo{Name: name, Ext: ext[1:]}) // Remove leading dot from ext
}

type deleteFontRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleDeleteFont(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req deleteFontRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.Name == "" || !safeNameRegex.MatchString(req.Name) {
		http.Error(w, "invalid font name", http.StatusBadRequest)
		return
	}

	fontsDir := s.getFontsDir()
	validExts := []string{".ttf", ".otf", ".woff", ".woff2"}

	deleted := false
	for _, ext := range validExts {
		path := filepath.Join(fontsDir, req.Name+ext)
		if err := os.Remove(path); err == nil {
			deleted = true
			break
		}
	}

	if !deleted {
		http.Error(w, "font not found", http.StatusNotFound)
		return
	}

	// If this was the active font, reset to default
	prefs, _ := s.loadPrefs()
	if prefs != nil && prefs.ActiveFont == req.Name {
		prefs.ActiveFont = "AdwaitaMono NF"
		s.savePrefs(prefs)
	}

	w.WriteHeader(http.StatusOK)
}

type setActiveFontRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleSetActiveFont(w http.ResponseWriter, r *http.Request) {
	var req setActiveFontRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	prefs, err := s.loadPrefs()
	if err != nil {
		prefs = &userPrefs{ActiveTheme: "Default"}
	}
	prefs.ActiveFont = req.Name

	if err := s.savePrefs(prefs); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}

// GetPrefs returns current preferences (for initial load)
func (s *Server) handleGetPrefs(w http.ResponseWriter, r *http.Request) {
	prefs, err := s.loadPrefs()
	if err != nil {
		prefs = &userPrefs{ActiveTheme: "Default", ActiveFont: "AdwaitaMono NF"}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(prefs)
}
