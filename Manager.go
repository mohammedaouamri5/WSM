package wsm

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Errors
var (
	ErrSocketNotFound      = errors.New("socket not found")
	ErrSocketAlreadyExists = errors.New("socket already exists")
	ErrConnDidntOpenYet    = errors.New("connection not opened yet")
)

// WSConn represents a WebSocket connection managed by the Manager.
type WSConn struct {
	Conn        *websocket.Conn // The underlying WebSocket connection (may be nil if scheduled only)
	SendCh      chan []byte     // Channel for outbound messages (to WebSocket)
	RecvCh      chan []byte     // Channel for inbound messages (from WebSocket)
	AutoClose   bool            // If true, auto-close on read/write errors
	CloseDelay  time.Duration   // Delay before closing after error (0 = immediate)
	ReadingHook func([]byte)    // Optional callback on every received message
}

// Manager handles multiple WebSocket connections by URL.
type Manager struct {
	mu      sync.RWMutex
	sockets map[string]*WSConn
}

// NewManager creates a new WebSocket connection manager.
func NewManager() *Manager {
	return &Manager{
		sockets: make(map[string]*WSConn),
	}
}

// NewWSConn creates a new WSConn with the given config.
func NewWSConn(conn *websocket.Conn, chanSize int, autoClose bool, closeDelay time.Duration, hook func([]byte)) *WSConn {
	if chanSize <= 0 {
		chanSize = 256
	}
	return &WSConn{
		Conn:        conn,
		SendCh:      make(chan []byte, chanSize),
		RecvCh:      make(chan []byte, chanSize),
		AutoClose:   autoClose,
		CloseDelay:  closeDelay,
		ReadingHook: hook,
	}
}

// Schedule reserves a socket with no live connection (Conn == nil).
func (m *Manager) Schedule(url string, chanSize int, closeDelay time.Duration, hook func([]byte)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sockets[url]; exists {
		return ErrSocketAlreadyExists
	}
	autoClose := closeDelay <= 0

	ws := NewWSConn(nil, chanSize, autoClose, closeDelay, hook)
	m.sockets[url] = ws
	return nil
}

// Open opens a new WebSocket connection under the given URL.
func (m *Manager) Open(url string, conn *websocket.Conn, closeDelay time.Duration) error {
	return m.OpenWithHook(url, conn, 256, closeDelay, nil)
}

// OpenWithHook opens a connection with buffer size, close delay, and optional ReadingHook.
func (m *Manager) OpenWithHook(url string, conn *websocket.Conn, chanSize int, closeDelay time.Duration, hook func([]byte)) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sockets[url]; exists {
		return ErrSocketAlreadyExists
	}
	autoClose := closeDelay <= 0

	ws := NewWSConn(conn, chanSize, autoClose, closeDelay, hook)
	m.sockets[url] = ws

	// Start background goroutines
	go m.startWriter(url, ws)
	go m.startReader(url, ws)

	return nil
}

// AddConn attaches a live connection to a scheduled socket.
func (m *Manager) AddConn(url string, conn *websocket.Conn) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	ws, exists := m.sockets[url]
	if !exists {
		return ErrSocketNotFound
	}
	if ws.Conn != nil {
		return ErrSocketAlreadyExists
	}

	ws.Conn = conn

	// Start background goroutines only once connection is set
	go m.startWriter(url, ws)
	go m.startReader(url, ws)

	return nil
}

// startWriter sends messages from SendCh to the WebSocket.
func (m *Manager) startWriter(url string, ws *WSConn) {
	defer func() { m.Close(url) }()

	for msg := range ws.SendCh {
		if ws.Conn == nil {
			return
		}
		if err := ws.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			if ws.AutoClose {
				if ws.CloseDelay > 0 {
					time.AfterFunc(ws.CloseDelay, func() { m.Close(url) })
				} else {
					m.Close(url)
				}
			}
			return
		}
	}
}

// startReader reads messages into RecvCh and triggers hook.
func (m *Manager) startReader(url string, ws *WSConn) {
	defer func() {
		if ws.AutoClose {
			if ws.CloseDelay > 0 {
				time.AfterFunc(ws.CloseDelay, func() { m.Close(url) })
			} else {
				m.Close(url)
			}
		}
	}()

	for {
		if ws.Conn == nil {
			return
		}
		_, msg, err := ws.Conn.ReadMessage()
		if err != nil {
			return
		}
		if ws.ReadingHook != nil {
			go ws.ReadingHook(msg)
		}
		ws.RecvCh <- msg
	}
}

// Send sends a message to the WebSocket.
func (m *Manager) Send(url string, data []byte) error {
	m.mu.RLock()
	ws, ok := m.sockets[url]
	m.mu.RUnlock()

	if !ok {
		return ErrSocketNotFound
	}
	if ws.Conn == nil {
		return ErrConnDidntOpenYet
	}

	ws.SendCh <- data
	return nil
}

// Receive waits for the next message (no context).
func (m *Manager) Receive(url string) ([]byte, error) {
	return m.ReceiveContext(context.Background(), url)
}

// ReceiveContext waits for the next message with a context.
func (m *Manager) ReceiveContext(ctx context.Context, url string) ([]byte, error) {
	m.mu.RLock()
	ws, ok := m.sockets[url]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrSocketNotFound
	}
	if ws.Conn == nil {
		return nil, ErrConnDidntOpenYet
	}

	select {
	case msg := <-ws.RecvCh:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the WebSocket connection.
func (m *Manager) Close(url string) error {
	m.mu.Lock()
	ws, ok := m.sockets[url]
	if !ok {
		m.mu.Unlock()
		return ErrSocketNotFound
	}
	delete(m.sockets, url)
	m.mu.Unlock()

	if ws.Conn != nil {
		_ = ws.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
		_ = ws.Conn.Close()
	}

	close(ws.SendCh)
	close(ws.RecvCh)

	return nil
}

// Exists checks if a socket with the given URL exists (scheduled or connected).
func (m *Manager) Exists(url string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sockets[url]
	return ok
}

// IsConnected checks if a socket has a live connection.
func (m *Manager) IsConnected(url string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ws, ok := m.sockets[url]
	return ok && ws.Conn != nil
}

// GetNames returns all registered socket URLs.
func (m *Manager) GetNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	names := make([]string, 0, len(m.sockets))
	for name := range m.sockets {
		names = append(names, name)
	}
	return names
}
