package main

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"os/signal"
	"time"

	"github.com/gorilla/websocket"
)

func main() {
	userID := fmt.Sprintf("%d", time.Now().UnixNano()%10000) // shorter ID
	u := url.URL{Scheme: "ws", Host: "localhost:9999", Path: "/ws/" + userID + "/notification"}
	log.Printf("Connecting to %s", u.String())

	// Dial
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("Dial error:", err)
	}
	defer c.Close()

	// Interrupt handler
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)

	// Done channel for reader
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
					log.Println("Read error:", err)
				}
				return
			}
			log.Printf("Received: %s", message)
		}
	}()

	// Heartbeat sender
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-done:
			return
		case t := <-ticker.C:
			msg := fmt.Sprintf("Hello from client %s at %s", userID, t.Format(time.RFC3339))
			err := c.WriteMessage(websocket.TextMessage, []byte(msg))
			if err != nil {
				log.Println("Write error:", err)
				return
			}
			log.Printf("Sent: %s", msg)
		case <-interrupt:
			log.Println("Interrupt: closing connection gracefully...")
			err := c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
			if err != nil {
				log.Println("Close write error:", err)
			}
			select {
			case <-done:
			case <-time.After(time.Second):
			}
			return
		}
	}
}
