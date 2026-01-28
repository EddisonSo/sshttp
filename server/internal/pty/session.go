package pty

import (
	"bufio"
	"fmt"
	"io"
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
	attached   bool
	scrollback *RingBuffer
	onKick     func() // Called when this connection is kicked by a new one
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
	}

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
				Attached:  session.attached,
			})
		}
		return true
	})
	return sessions
}

// Attach marks a session as attached (kicks out previous connection if any)
func (s *Session) Attach(onKick func()) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	// If already attached, kick out the old connection
	if s.attached && s.onKick != nil {
		// Call outside the lock to avoid deadlock
		kickCallback := s.onKick
		s.mu.Unlock()
		kickCallback()
		s.mu.Lock()
	}
	s.attached = true
	s.onKick = onKick
	return true
}

// Detach marks a session as detached
func (s *Session) Detach() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached = false
}

// IsAttached returns whether the session is attached
func (s *Session) IsAttached() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.attached
}

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
	return pty.Setsize(s.PTY, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

// Redraw sends SIGWINCH to the shell to force a prompt redraw
func (s *Session) Redraw() {
	if s.Cmd.Process != nil {
		s.Cmd.Process.Signal(syscall.SIGWINCH)
	}
}

func (s *Session) Wait() (int, error) {
	err := s.Cmd.Wait()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			return exitErr.ExitCode(), nil
		}
		return -1, err
	}
	return 0, nil
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
