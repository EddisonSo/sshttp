package api

import (
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/eddison/sshttp/server/internal/auth"
	"github.com/eddison/sshttp/server/internal/config"
	"github.com/eddison/sshttp/server/internal/mds"
	"github.com/eddison/sshttp/server/internal/middleware"
	"github.com/eddison/sshttp/server/internal/pty"
	"github.com/eddison/sshttp/server/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
)

// UserConn represents a WebSocket connection for a user
type UserConn struct {
	Conn    *websocket.Conn
	WriteMu *sync.Mutex
}

type Server struct {
	cfg            *config.Config
	store          store.Store
	webauthn       *auth.WebAuthnHandler
	tokenManager   *auth.TokenManager
	sessionManager *pty.SessionManager
	mds            *mds.Client
	rateLimiter    *middleware.RateLimiter
	embeddedFS     fs.FS

	// Track WebSocket connections per user for notifications
	userConns   map[string]map[string]*UserConn // userID -> clientID -> conn
	userConnsMu sync.RWMutex
}

func NewServer(cfg *config.Config, s store.Store, wa *auth.WebAuthnHandler, tm *auth.TokenManager, sm *pty.SessionManager, mdsClient *mds.Client) *Server {
	return &Server{
		cfg:            cfg,
		store:          s,
		webauthn:       wa,
		tokenManager:   tm,
		sessionManager: sm,
		mds:            mdsClient,
		rateLimiter:    middleware.NewRateLimiter(10, time.Minute),
		userConns:      make(map[string]map[string]*UserConn),
	}
}

// AddUserConn registers a WebSocket connection for a user
func (s *Server) AddUserConn(userID, clientID string, conn *websocket.Conn, writeMu *sync.Mutex) {
	s.userConnsMu.Lock()
	defer s.userConnsMu.Unlock()

	if s.userConns[userID] == nil {
		s.userConns[userID] = make(map[string]*UserConn)
	}
	s.userConns[userID][clientID] = &UserConn{Conn: conn, WriteMu: writeMu}
}

// RemoveUserConn unregisters a WebSocket connection for a user
func (s *Server) RemoveUserConn(userID, clientID string) {
	s.userConnsMu.Lock()
	defer s.userConnsMu.Unlock()

	if conns, ok := s.userConns[userID]; ok {
		delete(conns, clientID)
		if len(conns) == 0 {
			delete(s.userConns, userID)
		}
	}
}

// NotifySessionsChanged sends a notification to all of a user's connections
func (s *Server) NotifySessionsChanged(userID string) {
	s.userConnsMu.RLock()
	conns := s.userConns[userID]
	if conns == nil {
		s.userConnsMu.RUnlock()
		return
	}
	// Copy to avoid holding lock during writes
	connList := make([]*UserConn, 0, len(conns))
	for _, uc := range conns {
		connList = append(connList, uc)
	}
	s.userConnsMu.RUnlock()

	// Send notification to all connections
	frame := []byte{0x21} // FrameSessionsChange
	for _, uc := range connList {
		uc.WriteMu.Lock()
		uc.Conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		uc.Conn.WriteMessage(websocket.BinaryMessage, frame)
		uc.Conn.SetWriteDeadline(time.Time{}) // Clear deadline so data writes don't inherit it
		uc.WriteMu.Unlock()
	}
}

// SetEmbeddedFS sets the embedded filesystem for serving static files
func (s *Server) SetEmbeddedFS(fsys fs.FS) {
	s.embeddedFS = fsys
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	// Global middleware
	r.Use(middleware.SecurityHeaders)
	r.Use(middleware.Logger)
	r.Use(middleware.CORS(s.cfg.RPOrigins))

	// API routes
	r.Route("/v1", func(r chi.Router) {
		// Registration (rate limited)
		r.Route("/register", func(r chi.Router) {
			r.Use(s.rateLimiter.Middleware)
			r.Get("/info", s.handleRegisterInfo)
			r.Post("/begin", s.handleRegisterBegin)
			r.Post("/finish", s.handleRegisterFinish)
		})

		// Authentication (rate limited)
		r.Route("/auth", func(r chi.Router) {
			r.Use(s.rateLimiter.Middleware)
			r.Post("/begin", s.handleAuthBegin)
			r.Post("/finish", s.handleAuthFinish)
			r.Post("/logout", s.handleLogout)
		})

		// Protected routes
		r.Route("/shell", func(r chi.Router) {
			r.Use(middleware.Auth(s.tokenManager))
			r.Get("/sessions", s.handleListSessions)
			r.Post("/sessions", s.handleCreateSession)
			r.Post("/sessions/rename", s.handleRenameSession)
			r.Post("/sessions/delete", s.handleDeleteSession)
			r.Get("/stream", s.handleShellStream)
		})

		// Settings (protected)
		r.Route("/settings", func(r chi.Router) {
			r.Use(middleware.Auth(s.tokenManager))
			r.Get("/keys", s.handleListKeys)
			r.Post("/keys/delete", s.handleDeleteKey)
			r.Post("/keys/rename", s.handleRenameKey)
			r.Post("/keys/add/begin", s.handleAddKeyBegin)
			r.Post("/keys/add/finish", s.handleAddKeyFinish)

			// Customization
			r.Get("/prefs", s.handleGetPrefs)

			// Themes
			r.Get("/themes", s.handleListThemes)
			r.Get("/themes/get", s.handleGetTheme)
			r.Post("/themes/save", s.handleSaveTheme)
			r.Post("/themes/delete", s.handleDeleteTheme)
			r.Post("/themes/active", s.handleSetActiveTheme)

			// Fonts
			r.Get("/fonts", s.handleListFonts)
			r.Get("/fonts/get", s.handleGetFont)
			r.Post("/fonts/upload", s.handleUploadFont)
			r.Post("/fonts/delete", s.handleDeleteFont)
			r.Post("/fonts/active", s.handleSetActiveFont)
		})
	})

	// Serve static files and SPA
	s.serveStaticFiles(r)

	return r
}

// serveStaticFiles serves the frontend static files and handles SPA routing
func (s *Server) serveStaticFiles(r chi.Router) {
	staticDir := s.cfg.StaticDir

	// Check if static directory exists on disk
	useFilesystem := false
	if staticDir != "" {
		if _, err := os.Stat(staticDir); err == nil {
			useFilesystem = true
		}
	}

	// If no filesystem and no embedded FS, skip static file serving
	if !useFilesystem && s.embeddedFS == nil {
		return
	}

	// Create appropriate file server
	var fileServer http.Handler
	var staticFS fs.FS

	if useFilesystem {
		fileServer = http.FileServer(http.Dir(staticDir))
	} else {
		// Use embedded FS (files are under "static" subdirectory)
		subFS, err := fs.Sub(s.embeddedFS, "static")
		if err != nil {
			return
		}
		staticFS = subFS
		fileServer = http.FileServer(http.FS(staticFS))
	}

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path

		// Try to serve the file directly
		if useFilesystem {
			filePath := filepath.Join(staticDir, path)
			if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		} else {
			// Check embedded FS
			cleanPath := strings.TrimPrefix(path, "/")
			if cleanPath == "" {
				cleanPath = "index.html"
			}
			if f, err := staticFS.Open(cleanPath); err == nil {
				f.Close()
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// For SPA routes, serve index.html
		if !strings.HasPrefix(path, "/v1/") && !hasFileExtension(path) {
			if useFilesystem {
				http.ServeFile(w, r, filepath.Join(staticDir, "index.html"))
			} else {
				indexFile, err := staticFS.Open("index.html")
				if err != nil {
					http.NotFound(w, r)
					return
				}
				defer indexFile.Close()
				stat, _ := indexFile.Stat()
				content, _ := fs.ReadFile(staticFS, "index.html")
				http.ServeContent(w, r, "index.html", stat.ModTime(), strings.NewReader(string(content)))
			}
			return
		}

		// 404 for everything else
		http.NotFound(w, r)
	})
}

func hasFileExtension(path string) bool {
	ext := filepath.Ext(path)
	return ext != "" && ext != "."
}
