package websocket

import (
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
	log := logging.L().With(
		"component", "websocket",
		"module", "server",
		"func", "handleClient",
		"remote_addr", r.RemoteAddr,
		"path", r.URL.Path,
	)
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("ws upgrade failed", "error", err)
		return
	}
	log.Debug("client connected")
	s.hub.Add(c)
	go func() {
		defer s.hub.Remove(c)
		for {
			_, data, err := c.ReadMessage()
			if err != nil {
				log.Debug("client read closed", "error", err)
				return
			}
			s.hub.Broadcast(data)
		}
	}()
}

func (s *Server) handleMinecraft(w http.ResponseWriter, r *http.Request) {
	log := logging.L().With(
		"component", "websocket",
		"module", "server",
		"func", "handleMinecraft",
		"remote_addr", r.RemoteAddr,
		"path", r.URL.Path,
	)
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error("ws upgrade failed", "error", err)
		return
	}
	log.Info("minecraft connected via websocket")
	s.bridge.Attach(c)
}

func (s *Server) Start() error {
	log := logging.L().With(
		"component", "websocket",
		"module", "server",
		"func", "Start",
		"addr", s.addr,
	)
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleClient)
	mux.HandleFunc("/mc", s.handleMinecraft)
	log.Info("listening")
	return http.ListenAndServe(s.addr, mux)
}
