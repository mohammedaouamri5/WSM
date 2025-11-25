package wsm

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ErrSocketNotFound is returned when a socket with the given URL is not found.
var ErrSocketNotFound = errors.New("socket not found")

// ErrSocketAlreadyExists is returned when trying to open a socket with a URL that already exists.
var ErrSocketAlreadyExists = errors.New("socket already exists")

// WSConn represents a WebSocket connection managed by the Manager.
type WSConn struct {
	Conn        *websocket.Conn // The underlying WebSocket connection
	SendCh      chan []byte     // Channel for outbound messages (to WebSocket)
	RecvCh      chan []byte     // Channel for inbound messages (from WebSocket)
	AutoClose   bool            // If true, auto-close on read/write errors
	CloseDelay  time.Duration   // Delay before closing after error (0 = immediate)
	ReadingHook func([]byte)    // Optional callback on every received message

	// Debug
	DebugSendCh map[string]*debugPacket
	muSendCh    sync.RWMutex
	DebugRecvCh map[string]*debugPacket
	muRecvCh    sync.RWMutex
}

type debugCaller struct {
	Name string
	File string
	Line int
}

type debugSended struct {
	Time  *time.Time
	Error error
	Count int
}
type debugPacket struct {
	Data   any
	bytes  []byte
	Time   *time.Time
	Caller debugCaller
	Sended *debugSended
}

// Manager handles multiple WebSocket connections by URL.
// Provides thread-safe operations for opening, sending, receiving, and closing.
type Manager struct {
	DebugMode bool               // Enable debug mode
	mu        sync.RWMutex       // Protects access to the sockets map
	sockets   map[string]*WSConn // Map of URL → WebSocket connection
}

// NewManager creates a new WebSocket connection manager.
func NewManager(debug bool) *Manager {
	return &Manager{
		DebugMode: debug,
		sockets:   make(map[string]*WSConn),
	}
}

// NewWSConn creates a new WSConn with the given connection, channel buffer size,
// auto-close flag, close delay, and optional reading hook.
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
		DebugSendCh: nil,
		DebugRecvCh: nil,
	}
}

func (m *Manager) ToMap() any {
	__result := make(map[string]any, 0)
	if !m.DebugMode {
		__names := m.GetNames()
		__result["names"] = __names
		return __result
	}
	for _, name := range m.GetNames() {
		m.mu.RLock()
		defer m.mu.RUnlock()
		ws := __result[name]
		if ws == nil {
			continue
		}
		__result[name] = make(map[string]any, 0)
		__result[name].(map[string]any)["SendCh"] = m.sockets[name].DebugSendCh
		__result[name].(map[string]any)["RecvCh"] = m.sockets[name].DebugRecvCh

	}

	return __result

}

// GetNames returns a slice of all registered socket URLs.
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
func (m *Manager) Exists(url string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sockets[url]
	return ok
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

// startWriter sends messages from SendCh to the WebSocket.
func (m *Manager) startWriter(url string, ws *WSConn) {
	defer func() {
		m.Close(url) // Final cleanup
	}()

	for msg := range ws.SendCh {
		if err := ws.Conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			if m.DebugMode {
				ws.muSendCh.Lock()
				__packet := ws.DebugSendCh[HashToShortString(msg)]
				if __packet.Sended == nil {
					__packet.Sended = &debugSended{
						Time:  func() *time.Time { t := time.Now(); return &t }(), //&time.Now(),
						Error: err,
						Count: 1,
					}
				} else {
					__packet.Sended.Error = err
					__packet.Sended.Count++
					__packet.Sended.Time = func() *time.Time { t := time.Now(); return &t }() //&time.Now(),
				}
				ws.muSendCh.Unlock()
			}

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

// startReader reads messages and pushes them into RecvCh and/or triggers the hook.
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
		_, msg, err := ws.Conn.ReadMessage()
		if err != nil {
			return
		}

		// 🔥 Trigger hook if provided (non-blocking)
		if ws.ReadingHook != nil {
			go ws.ReadingHook(msg)
		}

		// Still deliver message into channel
		ws.RecvCh <- msg
	}
}

// Send sends a message to the WebSocket associated with the given URL.
func (m *Manager) Send(url string, data []byte) error {
	m.mu.RLock()
	ws, ok := m.sockets[url]
	m.mu.RUnlock()

	if !ok {
		return ErrSocketNotFound
	}

	if m.DebugMode {
		ws.muSendCh.Lock()
		if ws.DebugSendCh == nil {
			ws.DebugSendCh = make(map[string]*debugPacket)
		}
		name, file, line := getCallerInfo()
		__packet := &debugPacket{
			bytes: data,
			Time:  func() *time.Time { t := time.Now(); return &t }(), //&time.Now(),
			Caller: debugCaller{
				Name: name,
				File: file,
				Line: line,
			},
		}
		err := json.Unmarshal(data, &__packet.Data)
		if err != nil {
			__packet.Data = data
		}
		__hash := HashToShortString(data)
		ws.DebugSendCh[__hash] = __packet
		ws.muSendCh.Unlock()

	}

	ws.SendCh <- data

	return nil
}

// Receive waits for the next message from the socket at the given URL.
func (m *Manager) Receive(url string) ([]byte, error) {
	return m.ReceiveContext(context.Background(), url)
}

// ReceiveContext waits for the next message with a context (timeout/cancellation).
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

	_ = ws.Conn.Close()
	close(ws.SendCh)
	close(ws.RecvCh)

	return nil
}

// IsConnected checks whether a socket with the given URL is currently open.
func (m *Manager) IsConnected(url string) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.sockets[url]
	return ok
}
