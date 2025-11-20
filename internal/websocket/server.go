package websocket

import (
	"log"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/rotaria-smp/rotaria-bot/internal/mcbridge"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

type Server struct {
	addr   string
	hub    *Hub
	bridge *mcbridge.Bridge
}

func NewServer(addr string, hub *Hub, bridge *mcbridge.Bridge) *Server {
	return &Server{addr: addr, hub: hub, bridge: bridge}
}

var upgrader = websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }}

func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade: %v", err)
		return
	}
	s.hub.Add(c)
	go func() {
		defer s.hub.Remove(c)
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			s.hub.Broadcast(data)
		}
	}()
}

func (s *Server) handleMinecraft(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("mc upgrade: %v", err)
		return
	}
	log.Println("Minecraft connected via WebSocket")
	s.bridge.Attach(c)
}

func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleClient)
	mux.HandleFunc("/mc", s.handleMinecraft)
	logging.L().Info("websocket listening", "addr", s.addr)
	return http.ListenAndServe(s.addr, mux)
}
