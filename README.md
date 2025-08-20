
# WebSocket Manager (`wsm`)

A lightweight, thread-safe WebSocket connection manager built on top of [Gorilla WebSocket](https://github.com/gorilla/websocket).
It manages multiple connections by URL, with built-in send/receive channels, auto-close, and optional delayed shutdown.

---

## ✨ Features

* Manage multiple WebSocket connections by URL.
* Thread-safe operations (open, send, receive, close).
* Buffered channels for inbound (`RecvCh`) and outbound (`SendCh`) messages.
* Auto-close on read/write errors, with optional delay.
* Context-aware `ReceiveContext` for timeouts and cancellation.
* Graceful cleanup (`Close` closes connection, channels, and removes from manager).

---

## 📦 Installation

```bash
go get github.com/yourusername/yourrepo/wsm
```

---

## 🚀 Usage

### 1. Create a Manager

```go
manager := wsm.NewManager()
```

### 2. Open a Connection

```go
// Assume `conn` is a *websocket.Conn from an HTTP upgrade
err := manager.Open("/ws/user123/notification", conn, 2*time.Second)
if err != nil {
    log.Fatal(err)
}
```

### 3. Send a Message

```go
err = manager.Send("/ws/user123/notification", []byte("hello 👋"))
if err != nil {
    log.Println("send error:", err)
}
```

### 4. Receive a Message (blocking)

```go
msg, err := manager.Receive("/ws/user123/notification")
if err != nil {
    log.Println("recv error:", err)
}
fmt.Println("received:", string(msg))
```

### 5. Receive with Timeout

```go
ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()

msg, err := manager.ReceiveContext(ctx, "/ws/user123/notification")
if err != nil {
    log.Println("recv timeout:", err)
}
```

### 6. Close a Connection

```go
manager.Close("/ws/user123/notification")
```

---

## 🔧 API Overview

* `NewManager()` → create a new manager.
* `Open(url, conn, closeDelay)` → register a connection.
* `Send(url, data)` → send a message (blocks if buffer full).
* `Receive(url)` → blocking receive.
* `ReceiveContext(ctx, url)` → receive with context (timeout/cancel).
* `Close(url)` → close connection and cleanup.
* `Exists(url)` → check if a connection exists.
* `GetNames()` → list all active connection URLs.
* `IsConnected(url)` → check if connection is still alive.

---

## ⚠️ Notes

* **Backpressure**: `Send` and `Receive` block if channels are full. Use larger buffer size if needed.
* **AutoClose**: Connections automatically close on read/write errors after the optional `closeDelay`.
* **Safe cleanup**: Manager ensures connections and channels are closed exactly once.

---
