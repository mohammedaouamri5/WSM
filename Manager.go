package wsm

import (
	"context"
	"errors"
	"sync"

	"github.com/gorilla/websocket"
)

// ErrSocketNotFound is returned when a socket with the given URL is not found.
var ErrSocketNotFound = errors.New("socket not found")

// ErrSocketAlreadyExists is returned when trying to open a socket with a URL that already exists.
var ErrSocketAlreadyExists = errors.New("socket already exists")

// WSConn represents a WebSocket connection managed by the Manager.
type WSConn struct {
	Conn      *websocket.Conn // The underlying WebSocket connection
	SendCh    chan []byte     // Channel for outbound messages (to WebSocket)
	RecvCh    chan []byte     // Channel for inbound messages (from WebSocket)
	AutoClose bool            // If true, auto-close connection on read/write errors
}

// Manager handles multiple WebSocket connections by URL.
// It provides thread-safe operations for opening, sending, receiving, and closing connections.
type Manager struct {
	mu      sync.RWMutex     // Protects access to the sockets map
	sockets map[string]*WSConn // Map of URL → WebSocket connection
}

// NewManager creates a new WebSocket connection manager.
//
// Returns:
//   - *Manager: A new instance with an empty socket map.
func NewManager() *Manager {
	return &Manager{
		sockets: make(map[string]*WSConn),
	}
}

// NewWSConn creates a new WSConn with the given connection, channel buffer size, and auto-close behavior.
//
// Parameters:
//   - conn: The *websocket.Conn to manage.
//   - chanSize: Buffer size for SendCh and RecvCh. If <= 0, defaults to 256.
//   - autoClose: If true, the connection will be automatically closed on errors.
//
// Returns:
//   - *WSConn: A fully initialized WSConn instance.
func NewWSConn(conn *websocket.Conn, chanSize int, autoClose bool) *WSConn {
	if chanSize <= 0 {
		chanSize = 256
	}
	return &WSConn{
		Conn:      conn,
		SendCh:    make(chan []byte, chanSize),
		RecvCh:    make(chan []byte, chanSize),
		AutoClose: autoClose,
	}
}

// GetNames returns a slice of all registered socket URLs.
//
// Returns:
//   - []string: List of all currently open socket URLs (arbitrary order).
func (m *Manager) GetNames() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var names []string
	for name := range m.sockets {
		names = append(names, name)
	}
	return names
}

// Exists checks if a socket with the given URL is currently open.
//
// Parameters:
//   - url: The URL key to check.
//
// Returns:
//   - bool: true if the socket exists; false otherwise.
func (m *Manager) Exists(url string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sockets[url]
	return ok
}

// Open opens a new WebSocket connection under the given URL.
//
// Parameters:
//   - url: Unique identifier for the connection (e.g., ws://host:port/path).
//   - conn: The *websocket.Conn to manage.
//   - autoClose: If true, auto-close on read/write errors or remote closure.
//
// Returns:
//   - error: nil on success, ErrSocketAlreadyExists if URL is taken.
//
// The connection is managed in the background:
//   - Outgoing messages are sent via SendCh.
//   - Incoming messages are delivered via RecvCh.
//
// This method takes ownership of the connection. Do not use conn after calling Open.
func (m *Manager) Open(url string, conn *websocket.Conn, autoClose bool) error {
	return m.OpenWithSize(url, conn, autoClose, 256)
}

// OpenWithSize is like Open, but allows specifying the buffer size for internal channels.
//
// Parameters:
//   - url: The URL key for the connection.
//   - conn: The *websocket.Conn to manage.
//   - autoClose: If true, auto-close on errors.
//   - chanSize: Buffer size for SendCh and RecvCh. If <= 0, defaults to 256.
//
// Returns:
//   - error: nil on success, ErrSocketAlreadyExists if URL is already in use.
func (m *Manager) OpenWithSize(url string, conn *websocket.Conn, autoClose bool, chanSize int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.sockets[url]; exists {
		return ErrSocketAlreadyExists
	}

	ws := NewWSConn(conn, chanSize, autoClose)
	m.sockets[url] = ws

	// Start background goroutines
	go m.startWriter(url, ws)
	go m.startReader(url, ws)

	return nil
}

// startWriter runs in a goroutine and sends messages from SendCh to the WebSocket.
//
// Parameters:
//   - url: The URL key (used to clean up on error).
//   - ws: The managed WebSocket connection.
//
// Exits when:
//   - SendCh is closed (after Close)
//   - WriteMessage fails
//   - AutoClose triggers m.Close(url)
func (m *Manager) startWriter(url string, ws *WSConn) {
	defer func() {
		m.Close(url) // Ensure cleanup
	}()

	for msg := range ws.SendCh {
		if err := ws.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			if ws.AutoClose {
				m.Close(url)
			}
			return
		}
	}
}

// startReader runs in a goroutine and reads messages from the WebSocket into RecvCh.
//
// Parameters:
//   - url: The URL key for cleanup.
//   - ws: The managed WebSocket connection.
//
// Blocks if RecvCh is full (backpressure).
// Exits when ReadMessage returns an error (e.g., network issue or close).
// Calls m.Close(url) if AutoClose is enabled.
func (m *Manager) startReader(url string, ws *WSConn) {
	defer func() {
		if ws.AutoClose {
			m.Close(url)
		}
	}()

	for {
		_, msg, err := ws.Conn.ReadMessage()
		if err != nil {
			return
		}
		// Blocks if channel is full — applies backpressure
		ws.RecvCh <- msg
	}
}

// Send sends a message to the WebSocket associated with the given URL.
//
// Parameters:
//   - url: The URL of the target socket.
//   - data: The message payload (e.g., JSON in []byte).
//
// Returns:
//   - error: nil on success, ErrSocketNotFound if socket doesn't exist.
//
// This blocks if the send channel is full (backpressure).
func (m *Manager) Send(url string, data []byte) error {
	m.mu.RLock()
	ws, ok := m.sockets[url]
	m.mu.RUnlock()

	if !ok {
		return ErrSocketNotFound
	}

	// Blocks if SendCh is full — backpressure
	ws.SendCh <- data
	return nil
}

// Receive waits for the next message from the socket at the given URL.
//
// Parameters:
//   - url: The URL of the socket to receive from.
//
// Returns:
//   - []byte: The received message.
//   - error: ErrSocketNotFound if socket doesn't exist.
//
// Blocks until a message is available.
// Consider using ReceiveContext for timeouts.
func (m *Manager) Receive(url string) ([]byte, error) {
	return m.ReceiveContext(context.Background(), url)
}

// ReceiveContext waits for the next message with a context (timeout/cancellation).
//
// Parameters:
//   - ctx: Context for timeout or cancellation.
//   - url: The URL of the socket to receive from.
//
// Returns:
//   - []byte: The received message.
//   - error: ErrSocketNotFound if socket not found, or ctx.Err() if canceled/timed out.
func (m *Manager) ReceiveContext(ctx context.Context, url string) ([]byte, error) {
	m.mu.RLock()
	ws, ok := m.sockets[url]
	m.mu.RUnlock()

	if !ok {
		return nil, ErrSocketNotFound
	}

	select {
	case msg := <-ws.RecvCh:
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Close closes the WebSocket connection for the given URL.
//
// Parameters:
//   - url: The URL of the socket to close.
//
// Returns:
//   - error: nil on success, ErrSocketNotFound if socket doesn't exist.
//
// This method:
//   - Sends a WebSocket close message
//   - Closes the underlying connection
//   - Removes the socket from the manager
//   - Closes SendCh and RecvCh to stop goroutines
//
// Safe to call multiple times.
func (m *Manager) Close(url string) error {
	m.mu.Lock()
	ws, ok := m.sockets[url]
	if !ok {
		m.mu.Unlock()
		return ErrSocketNotFound
	}

	delete(m.sockets, url)
	m.mu.Unlock()

	// Graceful close
	_ = ws.Conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))

	// Close connection and channels
	_ = ws.Conn.Close()
	close(ws.SendCh)
	close(ws.RecvCh)

	return nil
}

// IsConnected checks whether a socket with the given URL is currently open.
//
// Parameters:
//   - url: The URL to check.
//
// Returns:
//   - bool: true if the socket exists and is connected; false otherwise.
func (m *Manager) IsConnected(url string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sockets[url]
	return ok
}
