package websocket

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

type Server struct {
	addr string
	hub  *Hub
}

func NewServer(addr string, hub *Hub) *Server {
	return &Server{addr: addr, hub: hub}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func (s *Server) handler(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("upgrade error: %v", err)
		return
	}
	s.hub.Add(conn)

	go func() {
		defer s.hub.Remove(conn)
		for {
			_, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			// Echo + broadcast
			s.hub.Broadcast(data)
		}
	}()
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handler)
	log.Printf("WebSocket listening on %s", s.addr)
	return http.ListenAndServe(s.addr, mux)
}
