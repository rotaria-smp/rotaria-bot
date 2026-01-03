package discord

import (
	"sync"
	"time"

	"github.com/rotaria-smp/discordwebhook"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

type WebhookMessage struct {
	Username  string
	Content   string
	AvatarURL string
	Timestamp time.Time
}

type WebhookQueue struct {
	mu           sync.Mutex
	messages     []*WebhookMessage
	interval     time.Duration
	maxQueueSize int
	webhookURL   string
	shutdown     chan struct{}
	wg           sync.WaitGroup
}

func NewWebhookQueue(webhookURL string, interval time.Duration, maxQueueSize int) *WebhookQueue {
	q := &WebhookQueue{
		messages:     make([]*WebhookMessage, 0),
		interval:     interval,
		maxQueueSize: maxQueueSize,
		webhookURL:   webhookURL,
		shutdown:     make(chan struct{}),
	}
	q.start()
	return q
}

func (q *WebhookQueue) start() {
	q.wg.Add(1)
	go q.worker()
}

func (q *WebhookQueue) worker() {
	defer q.wg.Done()
	ticker := time.NewTicker(q.interval)
	defer ticker.Stop()

	for {
		select {
		case <-q.shutdown:
			q.drainAll()
			return
		case <-ticker.C:
			q.processNext()
		}
	}
}

func (q *WebhookQueue) processNext() {
	q.mu.Lock()
	if len(q.messages) == 0 {
		q.mu.Unlock()
		return
	}

	msg := q.messages[0]
	q.messages = q.messages[1:]
	queueLen := len(q.messages)
	q.mu.Unlock()

	age := time.Since(msg.Timestamp)
	logging.L().Debug("sending queued webhook",
		"username", msg.Username,
		"remaining", queueLen,
		"age_seconds", age.Seconds(),
	)

	if err := q.send(msg); err != nil {
		logging.L().Error("webhook send failed",
			"username", msg.Username,
			"error", err,
		)
	}
}

func (q *WebhookQueue) send(msg *WebhookMessage) error {
	if q.webhookURL == "" {
		return nil
	}

	flag := discordwebhook.MessageFlagSuppressNotifications
	webhookMsg := discordwebhook.Message{
		Content:   &msg.Content,
		Username:  &msg.Username,
		AvatarURL: &msg.AvatarURL,
		Flags:     &flag,
	}

	return discordwebhook.SendMessage(q.webhookURL, webhookMsg)
}

func (q *WebhookQueue) Enqueue(username, content, avatarURL string) {
	if content == "" {
		return
	}

	q.mu.Lock()
	defer q.mu.Unlock()

	// Drop oldest if queue is full
	if len(q.messages) >= q.maxQueueSize {
		dropped := q.messages[0]
		q.messages = q.messages[1:]
		logging.L().Warn("webhook queue full, dropped oldest message",
			"dropped_username", dropped.Username,
			"dropped_content", dropped.Content,
			"dropped_age_seconds", time.Since(dropped.Timestamp).Seconds(),
			"queue_size", len(q.messages),
		)
	}

	msg := &WebhookMessage{
		Username:  username,
		Content:   content,
		AvatarURL: avatarURL,
		Timestamp: time.Now(),
	}

	q.messages = append(q.messages, msg)

	logging.L().Debug("webhook message enqueued",
		"username", username,
		"queue_length", len(q.messages),
	)
}

func (q *WebhookQueue) drainAll() {
	q.mu.Lock()
	msgs := make([]*WebhookMessage, len(q.messages))
	copy(msgs, q.messages)
	q.messages = nil
	q.mu.Unlock()

	if len(msgs) == 0 {
		return
	}

	logging.L().Info("draining webhook queue on shutdown", "count", len(msgs))

	for _, msg := range msgs {
		if err := q.send(msg); err != nil {
			logging.L().Error("failed to drain webhook message", "error", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func (q *WebhookQueue) Shutdown() {
	close(q.shutdown)
	q.wg.Wait()
}

func (q *WebhookQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.messages)
}