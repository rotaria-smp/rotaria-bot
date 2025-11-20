package mcbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"log"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Frame struct {
	Type  string `json:"type"`
	ID    string `json:"id,omitempty"`
	Body  string `json:"body,omitempty"`
	Topic string `json:"topic,omitempty"`
	Msg   string `json:"msg,omitempty"`
}

type Bridge struct {
	mu       sync.Mutex
	writeMu  sync.Mutex
	conn     *websocket.Conn
	pending  map[string]chan resp
	onEvent  func(topic, body string)
	shutdown chan struct{}
}

type resp struct {
	body string
	err  error
}

func New(onEvent func(topic, body string)) *Bridge {
	return &Bridge{
		pending:  make(map[string]chan resp),
		onEvent:  onEvent,
		shutdown: make(chan struct{}),
	}
}

func (b *Bridge) Attach(c *websocket.Conn) {
	b.mu.Lock()
	if b.conn != nil {
		_ = b.conn.Close()
	}
	b.conn = c
	b.mu.Unlock()
	go b.readLoop(c)
}

func (b *Bridge) readLoop(c *websocket.Conn) {
	for {
		_, data, err := c.ReadMessage()
		if err != nil {
			break
		}
		var f Frame
		if err := json.Unmarshal(data, &f); err != nil {
			log.Printf("bridge: bad json: %v", err)
			continue
		}
		log.Printf("bridge: recv frame type=%s id=%s topic=%s", f.Type, f.ID, f.Topic)

		switch f.Type {
		case "RES":
			b.mu.Lock()
			ch := b.pending[f.ID]
			delete(b.pending, f.ID)
			pend := len(b.pending)
			b.mu.Unlock()
			if ch != nil {
				ch <- resp{body: f.Body}
			} else {
				log.Printf("bridge: RES for unknown id=%s (pending=%d)", f.ID, pend)
			}

		case "ERR":
			b.mu.Lock()
			ch := b.pending[f.ID]
			delete(b.pending, f.ID)
			pend := len(b.pending)
			b.mu.Unlock()
			if ch != nil {
				ch <- resp{err: errors.New(f.Msg)}
			} else {
				log.Printf("bridge: ERR for unknown id=%s (pending=%d)", f.ID, pend)
			}

		case "EVT":
			b.mu.Lock()
			handler := b.onEvent
			b.mu.Unlock()
			if handler != nil {
				handler(f.Topic, f.Body)
			}

		default:
			log.Printf("bridge: unknown frame type=%s", f.Type)
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn == c {
		log.Printf("bridge: readLoop closing active conn; failing %d pending commands", len(b.pending))
		b.conn = nil
		for id, ch := range b.pending {
			delete(b.pending, id)
			ch <- resp{err: errors.New("bridge closed")}
		}
	} else {
		log.Printf("bridge: readLoop exit for stale conn")
	}
}

func (b *Bridge) SendCommand(ctx context.Context, body string) (string, error) {
	// Grab the conn and register pending
	b.mu.Lock()
	c := b.conn
	if c == nil {
		b.mu.Unlock()
		return "", errors.New("minecraft not connected")
	}
	id := newID()
	ch := make(chan resp, 1)
	b.pending[id] = ch
	b.mu.Unlock()

	// Serialize writes
	b.writeMu.Lock()
	err := c.WriteJSON(Frame{Type: "CMD", ID: id, Body: body})
	b.writeMu.Unlock()

	if err != nil {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return "", fmt.Errorf("write failed: %w", err)
	}

	log.Printf("bridge: sent CMD id=%s body=%q", id, body)

	tmr := time.NewTimer(10 * time.Second)
	defer tmr.Stop()

	select {
	case r := <-ch:
		if r.err != nil {
			log.Printf("bridge: CMD id=%s error=%v", id, r.err)
			return "", r.err
		}
		log.Printf("bridge: CMD id=%s result=%q", id, r.body)
		return r.body, nil

	case <-tmr.C:
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		log.Printf("bridge: CMD id=%s timeout", id)
		return "", errors.New("timeout")

	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		log.Printf("bridge: CMD id=%s ctxErr=%v", id, ctx.Err())
		return "", ctx.Err()
	}
}

func newID() string {
	return uuid.Must(uuid.NewRandom()).String()
}

func (b *Bridge) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil
}

func (b *Bridge) SetHandler(f func(topic, body string)) {
	b.mu.Lock()
	b.onEvent = f
	b.mu.Unlock()
}
