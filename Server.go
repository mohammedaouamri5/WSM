package main

import (
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	wsm "github.com/mohammedaouamri/WSM"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	r := gin.Default()
	manager := wsm.NewManager()

	// Handle new WebSocket connections
	r.GET("/ws/:userid/notification", func(c *gin.Context) {
		userid := c.Param("userid")

		conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
		if err != nil {
			c.String(500, "WebSocket upgrade failed")
			return
		}

		url := "/ws/" + userid + "/notification"
		manager.Open(url, conn, time.Millisecond ) // AutoClose = true

		// Dedicated reader per connection
	})

	// Broadcast loop
	go func() {
		ticker := time.NewTicker(1 * time.Second) // tick every 1s
		defer ticker.Stop()

		for t := range ticker.C {
			names := manager.GetNames()


		go func(urls []string) {
			for _ , u := range urls {
				msg, err := manager.Receive(u)
				if err != nil {
					fmt.Println("receive error:", err)
					return
				}
				fmt.Printf("[%s] %s\n", u, string(msg))
			}
		}(names)




			if len(names) == 0 {
				continue // no clients yet
			}

			// Pick a random client
			index := rand.Intn(len(names))
			target := names[index]

			// Send timestamp
			err := manager.Send(target, []byte(t.String()))
			if err != nil {
				fmt.Println("send error:", err)
			}
		}
	}()

	r.Run(":9999")
}
