package discord

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rotaria-smp/discordwebhook"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

var (
	chatLineRe = regexp.MustCompile(`^<([^>]+)>[ ]?(.*)$`)
	joinLineRe = regexp.MustCompile(`^\*\*([A-Za-z0-9_]+)\*\* joined the server\.$`)
	atEveryone = regexp.MustCompile(`(?i)@(everyone|here)\b`)
	nameRe     = regexp.MustCompile(`([A-Za-z0-9_]+)$`)
)

type webhookJob struct {
	username string
	content  string
	avatar   string
}

type identKey struct {
	username string
	avatar   string
}

var rotariaAvatarUrl string = "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp"

func (a *App) HandleMCEvent(topic, body string) {
	log := logging.L().With(
		"component", "discord",
		"module", "events",
		"func", "HandleMCEvent",
	)

	log.Debug("Handling event", "topic", topic)

	body = strings.TrimSpace(body)
	if body == "" {
		log.Debug("Event body was empty we will disregard it", "topic", topic)
		return
	}

	if topic == "status" {

		// Rate limit status updates to once per minute
		now := time.Now()
		if now.Sub(a.lastStatusUpdate) < time.Minute {
			log.With("topic", "status").Debug("skipping status update (rate limit)")
			return
		}

		if err := a.Session.UpdateGameStatus(0, body); err != nil {
			log.With("topic", "status").Error("failed to update presence", "error", err, "presence", body)
		} else {
			log.With("topic", "status").Debug("updated presence", "presence", body)
			a.lastStatusUpdate = now
		}
		return
	}

	// If a user joins the mc server, lets update the discord nick to match the ingame name
	if topic == "join" {
		log.With("topic", "join").Debug("player joined", "message", body)

		if m := joinLineRe.FindStringSubmatch(body); m != nil {
			mcName := m[1] // e.g. "limp4n__"
			log.With("topic", "join", "minecraft_name", mcName).Debug("parsed join username")

			// sync in background so we don't block event handling
			go a.handlePlayerJoinSync(mcName)
		}

		a.sendWebhook("Rotaria", body, rotariaAvatarUrl)
		return
	}

	if topic == "leave" || topic == "lifecycle" {
		a.sendWebhook("Rotaria", body, rotariaAvatarUrl)
		return
	}

	if topic == "chat" {
		log.With("topic", "chat").Debug("chat message received", "message", body)

		msg := body
		fullUsername := "server"
		minecraftName := "server"

		if m := chatLineRe.FindStringSubmatch(body); m != nil {
			// m[1] is e.g. "[Owner] Awiant"
			fullUsername = m[1]

			// Take only the last word as the MC name
			if n := nameRe.FindStringSubmatch(fullUsername); len(n) > 1 {
				minecraftName = n[1] // "Awiant"
			} else {
				minecraftName = fullUsername
			}

			msg = m[2]
		}

		// Defang @everyone mentions to a clearly broken form (no leading '@')
		msg = atEveryone.ReplaceAllString(msg, `$1`)

		if a.Blacklist != nil && a.Blacklist.Contains(msg) {
			log.With("topic", "chat", "minecraft_name", minecraftName).Info("blocked message (blacklist hit)", "message", msg)
			if a.Bridge.IsConnected() {
				ctx := context.Background()
				if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("kick %s", minecraftName)); err != nil {
					log.With("topic", "chat", "minecraft_name", minecraftName).Error("kick failed after blacklist hit", "error", err)
				}
			}
			return
		}

		if strings.TrimSpace(msg) == "" {
			return
		}

		a.sendWebhook(fullUsername, msg, fmt.Sprintf("https://minotar.net/avatar/%s/128.png", minecraftName))
	}
}

func (a *App) handlePlayerJoinSync(mcName string) {
	log := logging.L().With(
		"component", "discord",
		"module", "events",
		"func", "handlePlayerJoinSync",
		"minecraft_name", mcName,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uuid, err := a.NameMC.UsernameToUUID(mcName)
	if err != nil {
		// This will happen for offline/Bedrock/etc – just log and bail out
		log.Warn("UsernameToUUID failed", "error", err)
		return
	}

	log.Debug("resolved username to UUID", "uuid", uuid)

	entry, err := a.WLStore.GetByUUID(ctx, uuid)
	if err != nil {
		log.Error("DB lookup failed", "uuid", uuid, "error", err)
		return
	}
	if entry == nil {
		// They might not be whitelisted via Discord (e.g. whitelisted manually on server) this is bad
		log.Warn("no DB entry for UUID", "uuid", uuid)
		return
	}

	if entry.Username != mcName {
		log.Info("username changed, updating DB",
			"old_username", entry.Username,
			"new_username", mcName,
			"uuid", uuid,
			"discord_id", entry.DiscordID,
		)

		if err := a.WLStore.UpdateUser(ctx, entry.DiscordID, uuid, mcName); err != nil {
			log.Error("failed to update DB username",
				"uuid", uuid,
				"discord_id", entry.DiscordID,
				"error", err,
			)
			return
		}
	}

	if err := a.Session.GuildMemberNickname(a.Cfg.GuildID, entry.DiscordID, mcName); err != nil {
		log.Error("failed to update discord nickname",
			"uuid", uuid,
			"discord_id", entry.DiscordID,
			"error", err,
		)
		return
	}

	log.Info("updated discord nickname",
		"uuid", uuid,
		"discord_id", entry.DiscordID,
	)
}

func (a *App) sendWebhook(username, content, avatar string) {
	log := logging.L().With(
		"component", "discord",
		"module", "events",
		"func", "sendWebhook",
		"username", username,
	)
	if a.Cfg.DiscordWebhookURL == "" {
		log.Debug("DiscordWebhookURL is empty, not sending webhook")
		return
	}
	if strings.TrimSpace(content) == "" {
		log.Debug("webhook message is empty; will not send")
		return
	}

	job := webhookJob{username: username, content: content, avatar: avatar}

	select {
	case a.webhookQ <- job:
		// ok
	default:
		log.Warn("webhook queue full; dropping message")
	}
}

func (a *App) webhookWorker() {
	log := logging.L().With(
		"component", "discord",
		"module", "events",
		"func", "webhookWorker",
	)
	const (
		maxPerSecond = 2
		batchWindow  = 700 * time.Millisecond
		maxChars     = 1900
	)

	pace := time.NewTicker(time.Second / maxPerSecond)
	defer pace.Stop()

	timer := time.NewTimer(batchWindow)
	timer.Stop()

	for {
		first, ok := <-a.webhookQ
		if !ok {
			return
		}

		byUser := map[identKey][]string{}
		byUser[identKey{first.username, first.avatar}] = append(byUser[identKey{first.username, first.avatar}], first.content)

		// Start/reset window
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(batchWindow)

	collect:
		for {
			select {
			case j := <-a.webhookQ:
				k := identKey{j.username, j.avatar}
				byUser[k] = append(byUser[k], j.content)

			case <-timer.C:
				break collect
			}
		}

		// Send one message per user group, paced
		for k, msgs := range byUser {
			<-pace.C

			content := joinLimited(msgs, "\n", maxChars)
			username := k.username
			avatar := k.avatar
			flag := discordwebhook.MessageFlagSuppressNotifications

			msg := discordwebhook.Message{
				Content:   &content,
				Username:  &username,
				AvatarURL: &avatar,
				Flags:     &flag,
			}

			if err := discordwebhook.SendMessage(a.Cfg.DiscordWebhookURL, msg); err != nil {
				log.Error("webhook send failed", "error", err, "username", username)
			}
		}
	}
}

func joinLimited(parts []string, sep string, max int) string {
	var b strings.Builder
	for i, p := range parts {
		if i > 0 {
			if b.Len()+len(sep) > max {
				break
			}
			b.WriteString(sep)
		}
		if b.Len()+len(p) > max {
			b.WriteString("…")
			break
		}
		b.WriteString(p)
	}
	return b.String()
}
