package mcbridge

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

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
			continue
		}
		switch f.Type {
		case "RES":
			b.mu.Lock()
			ch := b.pending[f.ID]
			delete(b.pending, f.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- resp{body: f.Body}
			}
		case "ERR":
			b.mu.Lock()
			ch := b.pending[f.ID]
			delete(b.pending, f.ID)
			b.mu.Unlock()
			if ch != nil {
				ch <- resp{err: errors.New(f.Msg)}
			}
		case "EVT":
			if b.onEvent != nil {
				b.onEvent(f.Topic, f.Body)
			}
		}
	}
	b.mu.Lock()
	b.conn = nil
	for id, ch := range b.pending {
		delete(b.pending, id)
		ch <- resp{err: errors.New("bridge closed")}
	}
	b.mu.Unlock()
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

	_ = c.WriteJSON(Frame{Type: "CMD", ID: id, Body: body})

	tmr := time.NewTimer(10 * time.Second)
	defer tmr.Stop()
	select {
	case r := <-ch:
		if r.err != nil {
			return "", r.err
		}
		return r.body, nil
	case <-tmr.C:
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
		return "", errors.New("timeout")
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func newID() string {
	return time.Now().Format("20060102150405.000000000")
}

func (b *Bridge) IsConnected() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.conn != nil
}
