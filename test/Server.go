package main

import (
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	// Replace with your actual import path
	"github.com/mohammedaouamri5/WSM"
)

// Simulating your imported package
var _ = websocket.Upgrader{}
type Manager struct{} // placeholder
func NewManager() *Manager { return &Manager{} }

// We'll define the real Manager below or import from your package
// For now, assume you have:
//   import "github.com/mohammedaouamri5/WSM"
//   manager := WSM.NewManager()

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Replace this with your real Manager import
// import "github.com/mohammedaouamri5/WSM"
func main() {
	r := gin.Default()

	// ✅ Initialize the manager
	manager := NewManager() // Replace with: wsm.NewManager()

	// Handle new WebSocket connections
	r.GET("/ws/:userid/notification", func(c *gin.Context) {
		userid := c.Param("userid")
		url := "/ws/" + userid + "/notification"

		// Upgrade HTTP to WebSocket
		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed for user %s: %v", userid, err)
			return
		}
		defer conn.Close() // Safety: ensure close if something goes wrong

		// ✅ Open the connection in the manager
		// Note: Your Open signature was wrong — should be:
		// manager.Open(url, conn, true)
		if err := manager.Open(url, conn, true); err != nil {
			log.Printf("Failed to open socket for %s: %v", url, err)
			return
		}

		log.Printf("Client connected: %s", url)

		// 🛠 Optional: Add a reader loop here if you want to handle incoming messages
		// Otherwise, manager.Receive() will read them later
	})

	// Broadcast / Send loop
	go func() {
		ticker := time.NewTicker(3 * time.Second) // every 3s
		defer ticker.Stop()

		for range ticker.C {
			names := manager.GetNames()
			if len(names) == 0 {
				log.Println("No clients connected yet...")
				continue
			}

			// 🔁 Pick a random client
			index := rand.Intn(len(names))
			target := names[index]

			// 📤 Send timestamp
			msg := fmt.Sprintf("Server tick -> %s at %s", target, time.Now().Format("15:04:05"))
			if err := manager.Send(target, []byte(msg)); err != nil {
				log.Printf("Send error to %s: %v", target, err)
			} else {
				log.Printf("Sent to %s: %s", target, msg)
			}

			// 📥 Try to receive a message (optional)
			// This blocks until a message arrives unless you use context
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			if data, err := manager.ReceiveContext(ctx, target); err == nil {
				log.Printf("Received from %s: %s", target, string(data))
			}
			cancel()
		}
	}()

	log.Println("Server starting on :9999")
	if err := http.ListenAndServe(":9999", r); err != nil {
		log.Fatal("Server failed:", err)
	}
}
