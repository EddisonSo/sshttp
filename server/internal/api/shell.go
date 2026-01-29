package api

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/eddison/sshttp/server/internal/middleware"
	"github.com/gorilla/websocket"
)

var clientIDCounter uint64

// Frame types
const (
	FrameStdin          byte = 0x01
	FrameStdout         byte = 0x02
	FrameResize         byte = 0x04
	FrameExit           byte = 0x05
	FrameFileStart      byte = 0x10
	FrameFileChunk      byte = 0x11
	FrameFileAck        byte = 0x12
	FrameWriteState     byte = 0x20 // Notifies client of write access: payload[0] = 1 (writer) or 0 (viewer)
	FrameSessionsChange byte = 0x21 // Notifies client that session list has changed
	FrameResizeNotify   byte = 0x22 // Notifies client of new terminal size: [cols:u16][rows:u16]
	FrameClientCount    byte = 0x23 // Notifies client of connected client count: [count:u16]
)

// File ACK status codes
const (
	FileAckSuccess  byte = 0x00
	FileAckProgress byte = 0x01
	FileAckError    byte = 0x02
)

// File transfer limits
const (
	MaxFileSize   = 100 * 1024 * 1024 // 100MB
	FileChunkSize = 32 * 1024         // 32KB
)

// fileTransfer tracks an in-progress file upload
type fileTransfer struct {
	name     string
	size     uint32
	received uint32
	file     *os.File
	path     string
}

// validateFilename checks if a filename is safe for upload
func validateFilename(name string) error {
	if name == "" {
		return os.ErrInvalid
	}
	if strings.Contains(name, "/") || strings.Contains(name, "\\") {
		return os.ErrInvalid
	}
	if strings.Contains(name, "..") {
		return os.ErrInvalid
	}
	if strings.HasPrefix(name, ".") {
		return os.ErrInvalid
	}
	// Check for null bytes and other control characters
	for _, c := range name {
		if c < 32 {
			return os.ErrInvalid
		}
	}
	return nil
}

// sendFileAck sends a file acknowledgment frame
func sendFileAck(conn *websocket.Conn, status byte, message string) error {
	var frame []byte
	if message != "" {
		msgBytes := []byte(message)
		frame = make([]byte, 2+len(msgBytes))
		frame[0] = FrameFileAck
		frame[1] = status
		copy(frame[2:], msgBytes)
	} else {
		frame = []byte{FrameFileAck, status}
	}
	return conn.WriteMessage(websocket.BinaryMessage, frame)
}

func (s *Server) newUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		ReadBufferSize:  32 * 1024,
		WriteBufferSize: 32 * 1024,
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			if origin == "" {
				return true // Allow non-browser clients
			}
			for _, allowed := range s.cfg.RPOrigins {
				if origin == allowed {
					return true
				}
			}
			return false
		},
	}
}

type sessionInfo struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"createdAt"`
	Attached  bool      `json:"attached"`
}

type listSessionsResponse struct {
	Sessions []sessionInfo `json:"sessions"`
}

func (s *Server) handleListSessions(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	sessions := s.sessionManager.ListUserSessions(claims.UserID)
	resp := listSessionsResponse{Sessions: make([]sessionInfo, len(sessions))}
	for i, sess := range sessions {
		resp.Sessions[i] = sessionInfo{
			ID:        sess.ID,
			Name:      sess.Name,
			CreatedAt: sess.CreatedAt,
			Attached:  sess.Attached,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type createSessionRequest struct {
	Name string `json:"name,omitempty"`
}

type createSessionResponse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type renameSessionRequest struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type deleteSessionRequest struct {
	ID string `json:"id"`
}

func (s *Server) handleDeleteSession(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req deleteSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	session, ok := s.sessionManager.Get(req.ID)
	if !ok || session.UserID != claims.UserID {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	s.sessionManager.Delete(req.ID)

	// Notify other connections that sessions changed
	go s.NotifySessionsChanged(claims.UserID)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleRenameSession(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req renameSessionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	session, ok := s.sessionManager.Get(req.ID)
	if !ok || session.UserID != claims.UserID {
		http.Error(w, "session not found", http.StatusNotFound)
		return
	}

	session.Rename(req.Name)

	// Notify other connections that sessions changed
	go s.NotifySessionsChanged(claims.UserID)

	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleCreateSession(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req createSessionRequest
	json.NewDecoder(r.Body).Decode(&req) // Ignore errors, name is optional

	session, err := s.sessionManager.CreateNamed(claims.UserID, req.Name)
	if err != nil {
		http.Error(w, "failed to create session", http.StatusInternalServerError)
		return
	}

	// Notify other connections that sessions changed
	go s.NotifySessionsChanged(claims.UserID)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(createSessionResponse{
		ID:   session.ID,
		Name: session.Name,
	})
}

const (
	// WebSocket timeouts (generous for mobile which may suspend connections)
	wsWriteWait  = 10 * time.Second
	wsPongWait   = 60 * time.Second
	wsPingPeriod = 50 * time.Second
)

func (s *Server) handleShellStream(w http.ResponseWriter, r *http.Request) {
	claims := middleware.GetClaims(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Upgrade to WebSocket
	upgrader := s.newUpgrader()
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("websocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Set up pong handler to reset read deadline
	conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(wsPongWait))
		return nil
	})

	// Get session ID - required
	sessionID := r.URL.Query().Get("sessionId")
	if sessionID == "" {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "sessionId required"))
		return
	}

	// Try to connect to existing session
	session, ok := s.sessionManager.Get(sessionID)
	if !ok || session.UserID != claims.UserID {
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "session not found"))
		return
	}

	// Generate unique client ID
	clientID := fmt.Sprintf("client-%d", atomic.AddUint64(&clientIDCounter, 1))

	// Mutex for thread-safe WebSocket writes
	var writeMu sync.Mutex
	writeToClient := func(data []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		// Wrap data in stdout frame
		frame := make([]byte, 1+len(data))
		frame[0] = FrameStdout
		copy(frame[1:], data)
		return conn.WriteMessage(websocket.BinaryMessage, frame)
	}

	// Track active file transfer
	var activeTransfer *fileTransfer

	// Track if client has been registered with session
	clientRegistered := false
	isWriter := false // true if this client has write access

	// Channel to signal handler exit to goroutines
	done := make(chan struct{})

	// Helper to send write state to client
	sendWriteState := func(writer bool) {
		writeState := byte(0)
		if writer {
			writeState = 1
		}
		writeMu.Lock()
		conn.WriteMessage(websocket.BinaryMessage, []byte{FrameWriteState, writeState})
		writeMu.Unlock()
	}

	// Callback when this client is promoted to writer
	onPromoted := func() {
		isWriter = true
		sendWriteState(true)
		log.Printf("client %s promoted to writer for session %s", clientID, session.ID)
	}

	// Callback when this client loses write access (switched away from tab)
	onDemoted := func() {
		isWriter = false
		sendWriteState(false)
		log.Printf("client %s demoted from writer for session %s", clientID, session.ID)
	}

	// Callback when terminal size changes (tmux-style min dimensions)
	onSizeChange := func(cols, rows uint16) {
		frame := make([]byte, 5)
		frame[0] = FrameResizeNotify
		binary.BigEndian.PutUint16(frame[1:3], cols)
		binary.BigEndian.PutUint16(frame[3:5], rows)
		writeMu.Lock()
		conn.WriteMessage(websocket.BinaryMessage, frame)
		writeMu.Unlock()
	}

	// Callback when client count changes
	onClientCount := func(count int) {
		frame := make([]byte, 3)
		frame[0] = FrameClientCount
		binary.BigEndian.PutUint16(frame[1:3], uint16(count))
		writeMu.Lock()
		conn.WriteMessage(websocket.BinaryMessage, frame)
		writeMu.Unlock()
	}

	// On disconnect, remove client and cleanup
	defer func() {
		close(done) // Signal goroutines to stop
		session.RemoveClient(clientID)
		// Cleanup incomplete file transfer
		if activeTransfer != nil && activeTransfer.file != nil {
			activeTransfer.file.Close()
			os.Remove(activeTransfer.path)
			activeTransfer = nil
		}
		log.Printf("client %s disconnected from session %s (remaining: %d)", clientID, session.ID, session.ClientCount())
	}()

	// Register this connection for user-level notifications
	s.AddUserConn(claims.UserID, clientID, conn, &writeMu)
	defer s.RemoveUserConn(claims.UserID, clientID)

	log.Printf("client %s connected to session %s for user %s (total clients: %d)", clientID, session.ID, claims.Username, session.ClientCount()+1)

	// Watch for session exit - send exit frame and close WebSocket
	go func() {
		select {
		case <-session.ExitChan():
			// Send exit frame to this client
			writeMu.Lock()
			exitFrame := make([]byte, 5)
			exitFrame[0] = FrameExit
			binary.BigEndian.PutUint32(exitFrame[1:], uint32(session.ExitCode()))
			conn.WriteMessage(websocket.BinaryMessage, exitFrame)
			writeMu.Unlock()

			// Close WebSocket to terminate the read loop
			conn.Close()

			// Clean up session if this was the last client
			// (RemoveClient is called by defer, session deletion happens after all clients disconnect)
			if session.ClientCount() <= 1 {
				s.sessionManager.Delete(session.ID)
				log.Printf("session %s ended for user %s", session.ID, claims.Username)
			}
		case <-done:
			return
		}
	}()

	// Send periodic pings to detect dead connections
	go func() {
		ticker := time.NewTicker(wsPingPeriod)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				writeMu.Lock()
				conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				err := conn.WriteMessage(websocket.PingMessage, nil)
				conn.SetWriteDeadline(time.Time{}) // Clear deadline so data writes don't inherit it
				writeMu.Unlock()
				if err != nil {
					return
				}
			case <-session.ExitChan():
				return
			case <-done:
				return
			}
		}
	}()

	// Read from WebSocket, write to PTY
	for {
		messageType, data, err := conn.ReadMessage()
		if err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				log.Printf("websocket read error: %v", err)
			}
			return
		}

		if messageType != websocket.BinaryMessage || len(data) < 1 {
			continue
		}

		frameType := data[0]
		payload := data[1:]

		switch frameType {
		case FrameStdin:
			// Only the writer can send input to the terminal
			if !isWriter {
				continue
			}
			if _, err := session.Write(payload); err != nil {
				log.Printf("pty write error: %v", err)
				return
			}

		case FrameResize:
			if len(payload) >= 4 {
				cols := binary.BigEndian.Uint16(payload[0:2])
				rows := binary.BigEndian.Uint16(payload[2:4])

				if !clientRegistered {
					// Don't register with zero dimensions (hidden tab)
					if cols == 0 && rows == 0 {
						continue
					}
					// First resize - register client and send scrollback atomically
					// This ensures no output is missed or duplicated
					clientRegistered = true
					_, isWriter = session.AddClientWithScrollback(clientID, cols, rows, writeToClient, onPromoted, onDemoted, onSizeChange, onClientCount)

					// Notify client of their write status
					sendWriteState(isWriter)
					if isWriter {
						log.Printf("client %s is the writer for session %s", clientID, session.ID)
					} else {
						log.Printf("client %s is a viewer for session %s", clientID, session.ID)
					}
				} else {
					// Update client's size
					session.UpdateClientSize(clientID, cols, rows)
				}
			}

		case FrameFileStart:
			// Only the writer can upload files
			if !isWriter {
				sendFileAck(conn, FileAckError, "viewer cannot upload files")
				continue
			}

			// Format: [size:u32][name_len:u16][name:utf8]
			if len(payload) < 6 {
				sendFileAck(conn, FileAckError, "invalid frame")
				continue
			}

			// Cleanup any previous incomplete transfer
			if activeTransfer != nil && activeTransfer.file != nil {
				activeTransfer.file.Close()
				os.Remove(activeTransfer.path)
				activeTransfer = nil
			}

			fileSize := binary.BigEndian.Uint32(payload[0:4])
			nameLen := binary.BigEndian.Uint16(payload[4:6])

			if len(payload) < int(6+nameLen) {
				sendFileAck(conn, FileAckError, "invalid frame")
				continue
			}

			fileName := string(payload[6 : 6+nameLen])

			// Validate file size
			if fileSize > MaxFileSize {
				sendFileAck(conn, FileAckError, "file too large (max 100MB)")
				continue
			}

			// Validate filename
			if err := validateFilename(fileName); err != nil {
				sendFileAck(conn, FileAckError, "invalid filename")
				continue
			}

			// Get current working directory
			cwd, err := session.GetWorkingDir()
			if err != nil {
				log.Printf("get cwd error: %v", err)
				sendFileAck(conn, FileAckError, "failed to get working directory")
				continue
			}

			// Construct full path
			filePath := filepath.Join(cwd, fileName)

			// Ensure the resolved path is still within cwd (defense in depth)
			cleanPath := filepath.Clean(filePath)
			if !strings.HasPrefix(cleanPath, cwd) {
				sendFileAck(conn, FileAckError, "invalid path")
				continue
			}

			// Create file with O_EXCL to prevent overwrite
			f, err := os.OpenFile(cleanPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
			if err != nil {
				if os.IsExist(err) {
					sendFileAck(conn, FileAckError, "file already exists")
				} else {
					log.Printf("create file error: %v", err)
					sendFileAck(conn, FileAckError, "failed to create file")
				}
				continue
			}

			activeTransfer = &fileTransfer{
				name:     fileName,
				size:     fileSize,
				received: 0,
				file:     f,
				path:     cleanPath,
			}

			log.Printf("file upload started: %s (%d bytes)", fileName, fileSize)
			sendFileAck(conn, FileAckProgress, "")

		case FrameFileChunk:
			// Only the writer can upload files
			if !isWriter {
				sendFileAck(conn, FileAckError, "viewer cannot upload files")
				continue
			}

			// Format: [offset:u32][data...]
			if activeTransfer == nil {
				sendFileAck(conn, FileAckError, "no active transfer")
				continue
			}

			if len(payload) < 4 {
				sendFileAck(conn, FileAckError, "invalid chunk")
				continue
			}

			offset := binary.BigEndian.Uint32(payload[0:4])
			chunkData := payload[4:]

			// Verify offset matches expected position
			if offset != activeTransfer.received {
				log.Printf("chunk offset mismatch: expected %d, got %d", activeTransfer.received, offset)
				sendFileAck(conn, FileAckError, "offset mismatch")
				activeTransfer.file.Close()
				os.Remove(activeTransfer.path)
				activeTransfer = nil
				continue
			}

			// Write chunk
			n, err := activeTransfer.file.Write(chunkData)
			if err != nil {
				log.Printf("write chunk error: %v", err)
				sendFileAck(conn, FileAckError, "write failed")
				activeTransfer.file.Close()
				os.Remove(activeTransfer.path)
				activeTransfer = nil
				continue
			}

			activeTransfer.received += uint32(n)

			// Check if transfer complete
			if activeTransfer.received >= activeTransfer.size {
				activeTransfer.file.Close()
				log.Printf("file upload complete: %s", activeTransfer.name)
				sendFileAck(conn, FileAckSuccess, activeTransfer.name)
				activeTransfer = nil
			} else {
				// Send progress ACK
				sendFileAck(conn, FileAckProgress, "")
			}
		}
	}
}
