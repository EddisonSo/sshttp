package pty

import (
	"bufio"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"os/user"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// RingBuffer is a fixed-size circular buffer for storing recent terminal output
type RingBuffer struct {
	data  []byte
	size  int
	start int
	len   int
	mu    sync.Mutex
}

// NewRingBuffer creates a new ring buffer with the given capacity
func NewRingBuffer(capacity int) *RingBuffer {
	return &RingBuffer{
		data: make([]byte, capacity),
		size: capacity,
	}
}

// Write appends data to the ring buffer, overwriting oldest data if full
func (rb *RingBuffer) Write(p []byte) (n int, err error) {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	n = len(p)
	if n == 0 {
		return 0, nil
	}

	// If input is larger than buffer, only keep the last 'size' bytes
	if n >= rb.size {
		copy(rb.data, p[n-rb.size:])
		rb.start = 0
		rb.len = rb.size
		return n, nil
	}

	// Write data, wrapping around if necessary
	for i := 0; i < n; i++ {
		pos := (rb.start + rb.len) % rb.size
		if rb.len < rb.size {
			rb.len++
		} else {
			rb.start = (rb.start + 1) % rb.size
		}
		rb.data[pos] = p[i]
	}

	return n, nil
}

// Bytes returns a copy of the buffer contents in order
func (rb *RingBuffer) Bytes() []byte {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if rb.len == 0 {
		return nil
	}

	result := make([]byte, rb.len)
	if rb.start+rb.len <= rb.size {
		copy(result, rb.data[rb.start:rb.start+rb.len])
	} else {
		firstPart := rb.size - rb.start
		copy(result[:firstPart], rb.data[rb.start:])
		copy(result[firstPart:], rb.data[:rb.len-firstPart])
	}
	return result
}

// DefaultScrollbackSize is the default size of the scrollback buffer (64KB)
const DefaultScrollbackSize = 64 * 1024

// Client represents a connected WebSocket client
type Client struct {
	ID             string
	Cols           uint16
	Rows           uint16
	Writer         func([]byte) error
	OnPromoted     func()                    // Called when this client is promoted to writer
	OnDemoted      func()                    // Called when this client loses write access
	OnSizeChange   func(cols, rows uint16)   // Called when terminal size changes
	OnClientCount  func(count int)           // Called when client count changes
	IsWriter       bool                      // true if this client has write access to the terminal
}

type Session struct {
	ID        string
	UserID    string
	Name      string
	PTY       *os.File
	Cmd       *exec.Cmd
	CreatedAt time.Time
	LastInput time.Time

	mu         sync.Mutex
	closed     bool
	scrollback *RingBuffer

	// Multiplexing support
	clients        map[string]*Client
	clientsMu      sync.RWMutex
	writerClientID string // ID of the client with write access (empty = no writer)
	outputDone     chan struct{}
	outputOnce     sync.Once

	// Exit tracking
	exitChan chan struct{}
	exitCode int
	exitErr  error
}

type SessionManager struct {
	sessions sync.Map
	shell    string
}

func NewSessionManager() *SessionManager {
	// Get current user's shell from /etc/passwd
	shell := "/bin/bash"
	if u, err := user.Current(); err == nil {
		if userShell := getUserShell(u.Username); userShell != "" {
			shell = userShell
		}
	}
	return &SessionManager{
		shell: shell,
	}
}

// getUserShell reads the user's shell from /etc/passwd
func getUserShell(username string) string {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, username+":") {
			fields := strings.Split(line, ":")
			if len(fields) >= 7 {
				return fields[6] // shell is the 7th field
			}
		}
	}
	return ""
}

// Create spawns a new PTY session
func (m *SessionManager) Create(userID string) (*Session, error) {
	return m.CreateNamed(userID, "")
}

// CreateNamed spawns a new PTY session with a name
func (m *SessionManager) CreateNamed(userID, name string) (*Session, error) {
	cmd := exec.Command(m.shell, "-l")

	// Mimic SSH: start in home directory
	if home := os.Getenv("HOME"); home != "" {
		cmd.Dir = home
	}

	cmd.Env = append(os.Environ(),
		"TERM=xterm-256color",
	)

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("start pty: %w", err)
	}

	sessionID := generateID()
	if name == "" {
		name = fmt.Sprintf("Session %d", m.countUserSessions(userID)+1)
	}

	session := &Session{
		ID:         sessionID,
		UserID:     userID,
		Name:       name,
		PTY:        ptmx,
		Cmd:        cmd,
		CreatedAt:  time.Now(),
		LastInput:  time.Now(),
		scrollback: NewRingBuffer(DefaultScrollbackSize),
		clients:    make(map[string]*Client),
		outputDone: make(chan struct{}),
		exitChan:   make(chan struct{}),
	}

	// Start exit watcher goroutine
	go func() {
		err := cmd.Wait()
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				session.exitCode = exitErr.ExitCode()
			} else {
				session.exitCode = -1
				session.exitErr = err
			}
		}
		close(session.exitChan)
	}()

	m.sessions.Store(session.ID, session)
	return session, nil
}

func (m *SessionManager) countUserSessions(userID string) int {
	count := 0
	m.sessions.Range(func(key, value any) bool {
		session := value.(*Session)
		if session.UserID == userID {
			count++
		}
		return true
	})
	return count
}

// Get retrieves a session by ID
func (m *SessionManager) Get(id string) (*Session, bool) {
	val, ok := m.sessions.Load(id)
	if !ok {
		return nil, false
	}
	return val.(*Session), true
}

// Close terminates a session
func (m *SessionManager) Close(id string) error {
	val, ok := m.sessions.LoadAndDelete(id)
	if !ok {
		return nil
	}

	session := val.(*Session)
	return session.Close()
}

// Delete is an alias for Close
func (m *SessionManager) Delete(id string) error {
	return m.Close(id)
}

// CloseAllForUser closes all sessions for a user
func (m *SessionManager) CloseAllForUser(userID string) {
	m.sessions.Range(func(key, value any) bool {
		session := value.(*Session)
		if session.UserID == userID {
			m.Close(session.ID)
		}
		return true
	})
}

// CloseIdleSessions closes sessions that have been idle for too long
func (m *SessionManager) CloseIdleSessions(maxIdle time.Duration) {
	m.sessions.Range(func(key, value any) bool {
		session := value.(*Session)
		if time.Since(session.LastInput) > maxIdle {
			m.Close(session.ID)
		}
		return true
	})
}

// SessionInfo contains info about a session for listing
type SessionInfo struct {
	ID        string
	Name      string
	CreatedAt time.Time
	Attached  bool
}

// ListUserSessions returns all sessions for a user
func (m *SessionManager) ListUserSessions(userID string) []SessionInfo {
	var sessions []SessionInfo
	m.sessions.Range(func(key, value any) bool {
		session := value.(*Session)
		if session.UserID == userID && !session.closed {
			sessions = append(sessions, SessionInfo{
				ID:        session.ID,
				Name:      session.Name,
				CreatedAt: session.CreatedAt,
				Attached:  session.IsAttached(),
			})
		}
		return true
	})
	return sessions
}

// AddClient adds a new client connection to the session
func (s *Session) AddClient(clientID string, cols, rows uint16, writer func([]byte) error) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	s.clientsMu.Lock()
	s.clients[clientID] = &Client{
		ID:     clientID,
		Cols:   cols,
		Rows:   rows,
		Writer: writer,
	}
	s.clientsMu.Unlock()

	// Start output broadcaster on first client
	s.outputOnce.Do(func() {
		go s.broadcastOutput()
	})

	// Recalculate terminal size
	s.recalculateSize()
	return true
}

// AddClientWithScrollback adds a client and sends scrollback atomically
// This ensures no output is missed or duplicated between scrollback and live broadcasts
// Returns true if this client was granted write access (first client gets write access)
func (s *Session) AddClientWithScrollback(clientID string, cols, rows uint16, writer func([]byte) error, onPromoted func(), onDemoted func(), onSizeChange func(cols, rows uint16), onClientCount func(count int)) (ok bool, isWriter bool) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false, false
	}
	s.mu.Unlock()

	s.clientsMu.Lock()

	// Grant write access if no current writer or current writer is inactive
	var demotedWriter *Client
	grantWrite := s.writerClientID == ""
	if !grantWrite {
		if currentWriter, exists := s.clients[s.writerClientID]; !exists {
			// Writer no longer exists (stale)
			s.writerClientID = ""
			grantWrite = true
		} else if currentWriter.Cols == 0 && currentWriter.Rows == 0 {
			// Writer is inactive (hidden tab) — take over
			currentWriter.IsWriter = false
			demotedWriter = currentWriter
			s.writerClientID = ""
			grantWrite = true
		}
	}
	if grantWrite {
		s.writerClientID = clientID
	}

	// Add client
	s.clients[clientID] = &Client{
		ID:            clientID,
		Cols:          cols,
		Rows:          rows,
		Writer:        writer,
		OnPromoted:    onPromoted,
		OnDemoted:     onDemoted,
		OnSizeChange:  onSizeChange,
		OnClientCount: onClientCount,
		IsWriter:      grantWrite,
	}

	// Notify all clients of the new active count (excludes hidden tabs with 0x0 dims)
	count := s.activeClientCount()
	for _, c := range s.clients {
		if c.OnClientCount != nil {
			go c.OnClientCount(count)
		}
	}

	// Send scrollback to all clients so they can see previous terminal content
	// Since we use min dimensions across all clients (tmux strategy),
	// scrollback is always compatible with the current terminal size
	scrollback := s.scrollback.Bytes()
	if len(scrollback) > 0 {
		writer(scrollback)
	}

	s.clientsMu.Unlock()

	// Notify demoted writer (outside lock)
	if demotedWriter != nil && demotedWriter.OnDemoted != nil {
		demotedWriter.OnDemoted()
	}

	// Start output broadcaster on first client
	s.outputOnce.Do(func() {
		go s.broadcastOutput()
	})

	// Recalculate terminal size (uses min of all clients)
	s.recalculateSize()

	return true, grantWrite
}

// RemoveClient removes a client connection from the session
// If the removed client was the writer, promotes the next client to writer
func (s *Session) RemoveClient(clientID string) {
	s.clientsMu.Lock()

	wasWriter := s.writerClientID == clientID
	delete(s.clients, clientID)

	var promotedClient *Client

	// If the writer left, promote an active client (non-zero dims) first
	if wasWriter {
		s.writerClientID = ""
		// Prefer a client that is actively viewing (non-zero dimensions)
		for id, c := range s.clients {
			if c.Cols > 0 && c.Rows > 0 {
				c.IsWriter = true
				s.writerClientID = id
				promotedClient = c
				break
			}
		}
		// Fall back to any client if no active ones
		if promotedClient == nil {
			for id, c := range s.clients {
				c.IsWriter = true
				s.writerClientID = id
				promotedClient = c
				break
			}
		}
	}

	activeCount := s.activeClientCount()
	totalCount := len(s.clients)

	// Collect callbacks for client count notification
	var countCallbacks []func(int)
	for _, c := range s.clients {
		if c.OnClientCount != nil {
			countCallbacks = append(countCallbacks, c.OnClientCount)
		}
	}

	s.clientsMu.Unlock()

	// Notify promoted client (outside lock to avoid deadlock)
	if promotedClient != nil && promotedClient.OnPromoted != nil {
		promotedClient.OnPromoted()
	}

	// Notify remaining clients of active count
	for _, cb := range countCallbacks {
		cb(activeCount)
	}

	// Recalculate terminal size if clients remain
	if totalCount > 0 {
		s.recalculateSize()
	}
}

// CanWrite returns true if the specified client has write access
func (s *Session) CanWrite(clientID string) bool {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return s.writerClientID == clientID
}

// GetWriterClientID returns the current writer's client ID
func (s *Session) GetWriterClientID() string {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return s.writerClientID
}

// ClientCount returns the number of connected clients
func (s *Session) ClientCount() int {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}

// activeClientCount returns the number of clients with non-zero dimensions (must hold clientsMu)
func (s *Session) activeClientCount() int {
	count := 0
	for _, c := range s.clients {
		if c.Cols > 0 && c.Rows > 0 {
			count++
		}
	}
	return count
}

// UpdateClientSize updates a client's terminal dimensions
// If the writer sends 0x0 (e.g. switched to a different tab), promote another active client
func (s *Session) UpdateClientSize(clientID string, cols, rows uint16) {
	s.clientsMu.Lock()

	client, ok := s.clients[clientID]
	if !ok {
		s.clientsMu.Unlock()
		return
	}

	wasInactive := client.Cols == 0 && client.Rows == 0
	client.Cols = cols
	client.Rows = rows

	// If the writer just went inactive (0x0), promote another client that has real dimensions
	var demotedClient *Client
	var promotedClient *Client
	if cols == 0 && rows == 0 && s.writerClientID == clientID {
		client.IsWriter = false
		s.writerClientID = ""
		demotedClient = client
		for id, c := range s.clients {
			if id != clientID && c.Cols > 0 && c.Rows > 0 {
				c.IsWriter = true
				s.writerClientID = id
				promotedClient = c
				break
			}
		}
	}

	// If a client becomes active (non-zero dims) and there's no writer, claim it
	if cols > 0 && rows > 0 && s.writerClientID == "" {
		client.IsWriter = true
		s.writerClientID = clientID
		promotedClient = client
	}

	// Track if this client is becoming active (transitioning from 0x0 to real dims)
	becomingActive := wasInactive && cols > 0 && rows > 0
	becomingInactive := !wasInactive && cols == 0 && rows == 0

	// If active count changed, collect callbacks to notify
	var countCallbacks []func(int)
	var activeCount int
	if becomingActive || becomingInactive {
		activeCount = s.activeClientCount()
		for _, c := range s.clients {
			if c.OnClientCount != nil {
				countCallbacks = append(countCallbacks, c.OnClientCount)
			}
		}
	}

	s.clientsMu.Unlock()

	// Notify demoted client outside lock
	if demotedClient != nil && demotedClient.OnDemoted != nil {
		demotedClient.OnDemoted()
	}

	// Notify promoted client outside lock
	if promotedClient != nil && promotedClient.OnPromoted != nil {
		promotedClient.OnPromoted()
	}

	// Re-confirm write state to a client becoming active, so they have the correct
	// state immediately (they may have been promoted/demoted while their tab was hidden)
	if becomingActive && demotedClient == nil && promotedClient == nil {
		if client.IsWriter && client.OnPromoted != nil {
			client.OnPromoted()
		} else if !client.IsWriter && client.OnDemoted != nil {
			client.OnDemoted()
		}
	}

	// Notify all clients of updated active count
	for _, cb := range countCallbacks {
		cb(activeCount)
	}

	s.recalculateSize()
}

// Minimum terminal dimensions to ensure usability
const (
	MinTerminalCols uint16 = 40
	MinTerminalRows uint16 = 10
)

// recalculateSize sets terminal to the minimum dimensions across all clients
// This ensures content is formatted correctly for all connected screens (tmux strategy)
func (s *Session) recalculateSize() {
	s.clientsMu.RLock()

	if len(s.clients) == 0 {
		s.clientsMu.RUnlock()
		return
	}

	// Find minimum dimensions across all clients
	var minCols, minRows uint16 = 0xFFFF, 0xFFFF
	for _, client := range s.clients {
		if client.Cols > 0 && client.Cols < minCols {
			minCols = client.Cols
		}
		if client.Rows > 0 && client.Rows < minRows {
			minRows = client.Rows
		}
	}

	// Enforce minimum dimensions for usability
	if minCols < MinTerminalCols {
		minCols = MinTerminalCols
	}
	if minRows < MinTerminalRows {
		minRows = MinTerminalRows
	}

	if minCols == 0xFFFF || minRows == 0xFFFF {
		s.clientsMu.RUnlock()
		return
	}

	// Collect callbacks to notify (to call outside the lock)
	var callbacks []func(cols, rows uint16)
	for _, client := range s.clients {
		if client.OnSizeChange != nil {
			callbacks = append(callbacks, client.OnSizeChange)
		}
	}

	s.clientsMu.RUnlock()

	// Resize the PTY
	s.Resize(minCols, minRows)

	// Notify all clients of the new size
	for _, cb := range callbacks {
		cb(minCols, minRows)
	}
}

// Broadcast sends data to all connected clients
func (s *Session) Broadcast(data []byte) {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()

	for _, client := range s.clients {
		client.Writer(data)
	}
}

// broadcastOutput reads from PTY and broadcasts to all clients
func (s *Session) broadcastOutput() {
	defer close(s.outputDone)
	buf := make([]byte, 32*1024)
	for {
		n, err := s.PTY.Read(buf)
		if err != nil {
			return
		}
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])

			// Hold clientsMu while writing to scrollback AND broadcasting
			// This ensures atomic operation with AddClientWithScrollback
			s.clientsMu.Lock()
			s.scrollback.Write(data)
			clientCount := len(s.clients)
			for id, client := range s.clients {
				if err := client.Writer(data); err != nil {
					log.Printf("[broadcast] Error writing to client %s: %v", id, err)
				}
			}
			log.Printf("[broadcast] Sent %d bytes to %d clients", n, clientCount)
			s.clientsMu.Unlock()
		}
	}
}

// IsAttached returns whether any client is attached
func (s *Session) IsAttached() bool {
	return s.ClientCount() > 0
}

// Legacy Attach for compatibility - always succeeds if not closed
func (s *Session) Attach() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.closed
}

// Legacy Detach for compatibility - no-op, use RemoveClient instead
func (s *Session) Detach() {}

// Rename changes the session name
func (s *Session) Rename(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Name = name
}

// Session methods

func (s *Session) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}
	s.closed = true

	s.PTY.Close()
	if s.Cmd.Process != nil {
		s.Cmd.Process.Kill()
	}
	return nil
}

func (s *Session) Write(p []byte) (int, error) {
	s.mu.Lock()
	s.LastInput = time.Now()
	s.mu.Unlock()
	return s.PTY.Write(p)
}

func (s *Session) Read(p []byte) (int, error) {
	n, err := s.PTY.Read(p)
	if n > 0 {
		s.scrollback.Write(p[:n])
	}
	return n, err
}

// Scrollback returns the buffered terminal output
func (s *Session) Scrollback() []byte {
	return s.scrollback.Bytes()
}

func (s *Session) Resize(cols, rows uint16) error {
	err := pty.Setsize(s.PTY, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
	if err == nil {
		// Send SIGWINCH to notify shell of size change so it redraws
		s.Redraw()
	}
	return err
}

// Redraw sends SIGWINCH to the shell to force a prompt redraw
func (s *Session) Redraw() {
	if s.Cmd.Process != nil {
		s.Cmd.Process.Signal(syscall.SIGWINCH)
	}
}


// ExitChan returns a channel that's closed when the shell process exits
func (s *Session) ExitChan() <-chan struct{} {
	return s.exitChan
}

// ExitCode returns the exit code (valid after ExitChan is closed)
func (s *Session) ExitCode() int {
	return s.exitCode
}

// Wait blocks until the shell process exits and returns the exit code
func (s *Session) Wait() (int, error) {
	<-s.exitChan
	return s.exitCode, s.exitErr
}

// Reader returns an io.Reader for the PTY output
func (s *Session) Reader() io.Reader {
	return s.PTY
}

// Writer returns an io.Writer for the PTY input
func (s *Session) Writer() io.Writer {
	return s
}

// GetWorkingDir returns the current working directory of the shell process
func (s *Session) GetWorkingDir() (string, error) {
	if s.Cmd.Process == nil {
		return "", fmt.Errorf("process not running")
	}
	pid := s.Cmd.Process.Pid
	cwd, err := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	if err != nil {
		return "", fmt.Errorf("read cwd: %w", err)
	}
	return cwd, nil
}

func generateID() string {
	return fmt.Sprintf("sess-%d", time.Now().UnixNano())
}
