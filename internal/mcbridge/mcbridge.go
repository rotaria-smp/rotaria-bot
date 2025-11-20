package mcbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
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
			logging.L().Warn("bridge bad json", "err", err)
			continue
		}
		logging.L().Debug("bridge recv", "type", f.Type, "id", f.ID, "topic", f.Topic)

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
				logging.L().Debug("bridge RES for unknown id", "id", f.ID, "pending", pend)
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
				logging.L().Error("bridge ERR for unknown id", "id", f.ID, "pending", pend)
			}

		case "EVT":
			b.mu.Lock()
			handler := b.onEvent
			b.mu.Unlock()
			if handler != nil {
				handler(f.Topic, f.Body)
			}

		default:
			logging.L().Debug("bridge: unknown frame type", "type", f.Type)
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.conn == c {
		logging.L().Warn("bridge: readLoop closing active conn; failing pending commands", "pending", len(b.pending))
		b.conn = nil
		for id, ch := range b.pending {
			delete(b.pending, id)
			ch <- resp{err: errors.New("bridge closed")}
		}
	} else {
		logging.L().Warn("bridge: readLoop exit for stale conn")
	}
}

func (b *Bridge) SendCommand(ctx context.Context, body string) (string, error) {
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

	logging.L().Info("bridge sent CMD", "id", id, "body", body)

	tmr := time.NewTimer(10 * time.Second)
	defer tmr.Stop()

	select {
	case r := <-ch:
		if r.err != nil {
			logging.L().Error("bridge CMD error", "id", id, "err", r.err)
			return "", r.err
		}
		logging.L().Info("bridge CMD result", "id", id, "result", r.body)
		return r.body, nil

	case <-tmr.C:
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		logging.L().Warn("bridge CMD timeout", "id", id)
		return "", errors.New("timeout")

	case <-ctx.Done():
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		logging.L().Warn("bridge CMD context done", "id", id, "err", ctx.Err())
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
