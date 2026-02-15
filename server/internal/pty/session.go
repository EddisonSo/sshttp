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

// ClientActivity represents whether a client is actively viewing the terminal
type ClientActivity int

const (
	ClientInactive ClientActivity = iota
	ClientActive
)

// notifications batches outbound callbacks to dispatch after lock release
type notifications struct {
	calls []func()
}

func (n *notifications) add(fn func()) {
	n.calls = append(n.calls, fn)
}

func (n *notifications) dispatch() {
	for _, fn := range n.calls {
		fn()
	}
}

// Client represents a connected WebSocket client
type Client struct {
	ID                 string
	Cols               uint16
	Rows               uint16
	Activity           ClientActivity
	JoinedAt           time.Time
	Writer             func([]byte) error
	OnWriteStateChange func(isWriter bool)     // Called when write access changes
	OnSizeChange       func(cols, rows uint16) // Called when terminal size changes
	OnClientCount      func(count int)         // Called when client count changes
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
	clientOrder    []string // insertion-ordered client IDs for deterministic iteration
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

// RegisterClient adds a client and sends scrollback atomically.
// The onWriteStateChange callback fires after the lock is released.
func (s *Session) RegisterClient(clientID string, cols, rows uint16, writer func([]byte) error, onWriteStateChange func(bool), onSizeChange func(uint16, uint16), onClientCount func(int)) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.mu.Unlock()

	var notify notifications

	s.clientsMu.Lock()

	s.clients[clientID] = &Client{
		ID:                 clientID,
		Cols:               cols,
		Rows:               rows,
		Activity:           ClientActive,
		JoinedAt:           time.Now(),
		Writer:             writer,
		OnWriteStateChange: onWriteStateChange,
		OnSizeChange:       onSizeChange,
		OnClientCount:      onClientCount,
	}
	s.clientOrder = append(s.clientOrder, clientID)

	s.electWriter(&notify)

	// Always notify the new client of their write state
	if onWriteStateChange != nil {
		isWriter := s.writerClientID == clientID
		cb := onWriteStateChange
		w := isWriter
		notify.add(func() { cb(w) })
	}

	// Notify all clients of new active count
	count := s.countActive()
	for _, c := range s.clients {
		if c.OnClientCount != nil {
			cb := c.OnClientCount
			cnt := count
			notify.add(func() { cb(cnt) })
		}
	}

	// Send scrollback atomically under lock
	scrollback := s.scrollback.Bytes()
	if len(scrollback) > 0 {
		writer(scrollback)
	}

	s.clientsMu.Unlock()

	// Start output broadcaster on first client
	s.outputOnce.Do(func() {
		go s.broadcastOutput()
	})

	notify.dispatch()

	s.recalculateSize()
	return true
}

// SetClientActivity handles all client activity transitions.
//   - Active->Inactive: writer is transferred if needed
//   - Inactive->Active: writer election runs, write state is re-confirmed
//   - Active->Active: fast path, just updates dimensions
func (s *Session) SetClientActivity(clientID string, activity ClientActivity, cols, rows uint16) {
	var notify notifications

	s.clientsMu.Lock()

	client, ok := s.clients[clientID]
	if !ok {
		s.clientsMu.Unlock()
		return
	}

	oldActivity := client.Activity
	client.Cols = cols
	client.Rows = rows
	client.Activity = activity

	activityChanged := oldActivity != activity

	if activityChanged {
		s.electWriter(&notify)

		// Re-confirm write state when becoming active
		if activity == ClientActive && client.OnWriteStateChange != nil {
			isWriter := s.writerClientID == clientID
			cb := client.OnWriteStateChange
			w := isWriter
			notify.add(func() { cb(w) })
		}

		// Notify all clients of updated active count
		count := s.countActive()
		for _, c := range s.clients {
			if c.OnClientCount != nil {
				cb := c.OnClientCount
				cnt := count
				notify.add(func() { cb(cnt) })
			}
		}
	}

	s.clientsMu.Unlock()

	notify.dispatch()
	s.recalculateSize()
}

// RemoveClient removes a client and runs writer election if needed.
func (s *Session) RemoveClient(clientID string) {
	var notify notifications

	s.clientsMu.Lock()

	delete(s.clients, clientID)
	for i, id := range s.clientOrder {
		if id == clientID {
			s.clientOrder = append(s.clientOrder[:i], s.clientOrder[i+1:]...)
			break
		}
	}

	if s.writerClientID == clientID {
		s.writerClientID = ""
	}

	s.electWriter(&notify)

	count := s.countActive()
	for _, c := range s.clients {
		if c.OnClientCount != nil {
			cb := c.OnClientCount
			cnt := count
			notify.add(func() { cb(cnt) })
		}
	}

	totalCount := len(s.clients)

	s.clientsMu.Unlock()

	notify.dispatch()

	if totalCount > 0 {
		s.recalculateSize()
	}
}

// electWriter is the single centralized writer election function.
// Must be called with clientsMu held. Batches notifications into notify.
func (s *Session) electWriter(notify *notifications) {
	// If current writer is still active, no-op
	if s.writerClientID != "" {
		if writer, exists := s.clients[s.writerClientID]; exists && writer.Activity == ClientActive {
			return
		}
	}

	oldWriterID := s.writerClientID
	newWriterID := ""

	// Find first active client in insertion order
	for _, id := range s.clientOrder {
		if c, exists := s.clients[id]; exists && c.Activity == ClientActive {
			newWriterID = id
			break
		}
	}

	// Fallback: any client if no active ones
	if newWriterID == "" {
		for _, id := range s.clientOrder {
			if _, exists := s.clients[id]; exists {
				newWriterID = id
				break
			}
		}
	}

	if newWriterID == oldWriterID {
		return
	}

	s.writerClientID = newWriterID

	// Notify old writer they lost access
	if oldWriterID != "" {
		if oldWriter, exists := s.clients[oldWriterID]; exists && oldWriter.OnWriteStateChange != nil {
			cb := oldWriter.OnWriteStateChange
			notify.add(func() { cb(false) })
		}
	}

	// Notify new writer they gained access
	if newWriterID != "" {
		if newWriter, exists := s.clients[newWriterID]; exists && newWriter.OnWriteStateChange != nil {
			cb := newWriter.OnWriteStateChange
			notify.add(func() { cb(true) })
		}
	}
}

// CanWrite returns true if the specified client has write access
func (s *Session) CanWrite(clientID string) bool {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return s.writerClientID == clientID
}

// ClientCount returns the number of connected clients
func (s *Session) ClientCount() int {
	s.clientsMu.RLock()
	defer s.clientsMu.RUnlock()
	return len(s.clients)
}

// countActive returns the number of active clients (must hold clientsMu)
func (s *Session) countActive() int {
	count := 0
	for _, c := range s.clients {
		if c.Activity == ClientActive {
			count++
		}
	}
	return count
}

// Minimum terminal dimensions to ensure usability
const (
	MinTerminalCols uint16 = 40
	MinTerminalRows uint16 = 10
)

// recalculateSize sets terminal to the minimum dimensions across all active clients
// This ensures content is formatted correctly for all connected screens (tmux strategy)
func (s *Session) recalculateSize() {
	s.clientsMu.RLock()

	if len(s.clients) == 0 {
		s.clientsMu.RUnlock()
		return
	}

	// Find minimum dimensions across active clients
	var minCols, minRows uint16 = 0xFFFF, 0xFFFF
	for _, client := range s.clients {
		if client.Activity == ClientActive {
			if client.Cols < minCols {
				minCols = client.Cols
			}
			if client.Rows < minRows {
				minRows = client.Rows
			}
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
			// This ensures atomic operation with RegisterClient
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
