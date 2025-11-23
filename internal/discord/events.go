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
	atEveryone = regexp.MustCompile(`@everyone`)
	nameRe     = regexp.MustCompile(`([A-Za-z0-9_]+)$`)
)

var rotariaAvatarUrl string = "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp"

func (a *App) HandleMCEvent(topic, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}

	if topic == "status" {

		// Rate limit status updates to once per minute
		now := time.Now()
		if now.Sub(a.lastStatusUpdate) < time.Minute {
			logging.L().Debug("HandleMCEvent: skipping status update due to rate limit")
			return
		}

		if err := a.Session.UpdateGameStatus(0, body); err != nil {
			logging.L().Error("HandleMCEvent: failed to update presence", "error", err)
		} else {
			logging.L().Debug("HandleMCEvent: updated presence", "presence", body)
			a.lastStatusUpdate = now
		}
		return
	}

	// If a user joins the mc server, lets update the discord nick to match the ingame name
	if topic == "join" {
		logging.L().Debug("Player joined", "message", body)

		if m := joinLineRe.FindStringSubmatch(body); m != nil {
			mcName := m[1] // e.g. "limp4n__"
			logging.L().Debug("Parsed join username", "minecraft_name", mcName)

			// sync in background so we don't block event handling
			go a.handlePlayerJoinSync(mcName)
		}

		a.sendWebhook("Rotaria", body, "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp")
		return
	}

	if topic == "leave" || topic == "lifecycle" {
		a.sendWebhook("Rotaria", body, rotariaAvatarUrl)
		return
	}

	if topic == "chat" {
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
		msg = atEveryone.ReplaceAllString(msg, "everyone")

		if a.Blacklist != nil && a.Blacklist.Contains(msg) {
			logging.L().Info("Blocked message from user (blacklist hit)", "message", msg, "user", minecraftName)
			if a.Bridge.IsConnected() {
				ctx := context.Background()
				if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("kick %s", minecraftName)); err != nil {
					logging.L().Error("kick failed after blacklist hit", "minecraft_name", minecraftName, "error", err)
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
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uuid, err := a.NameMC.UsernameToUUID(mcName)
	if err != nil {
		// This will happen for offline/Bedrock/etc – just log and bail out
		logging.L().Warn("handlePlayerJoinSync: UsernameToUUID failed",
			"minecraft_name", mcName,
			"error", err,
		)
		return
	}

	logging.L().Debug("handlePlayerJoinSync: resolved username to UUID",
		"minecraft_name", mcName,
		"uuid", uuid,
	)

	entry, err := a.WLStore.GetByUUID(ctx, uuid)
	if err != nil {
		logging.L().Error("handlePlayerJoinSync: DB lookup failed",
			"minecraft_name", mcName,
			"uuid", uuid,
			"error", err,
		)
		return
	}
	if entry == nil {
		// They might not be whitelisted via Discord (e.g. whitelisted manually on server) this is bad
		logging.L().Warn("handlePlayerJoinSync: no DB entry for UUID",
			"minecraft_name", mcName,
			"uuid", uuid,
		)
		return
	}

	if entry.Username != mcName {
		logging.L().Info("handlePlayerJoinSync: username changed, updating DB",
			"old_username", entry.Username,
			"new_username", mcName,
			"uuid", uuid,
			"discord_id", entry.DiscordID,
		)

		if err := a.WLStore.UpdateUser(ctx, entry.DiscordID, uuid, mcName); err != nil {
			logging.L().Error("handlePlayerJoinSync: failed to update DB username",
				"minecraft_name", mcName,
				"uuid", uuid,
				"discord_id", entry.DiscordID,
				"error", err,
			)
			return
		}
	}

	if err := a.Session.GuildMemberNickname(a.Cfg.GuildID, entry.DiscordID, mcName); err != nil {
		logging.L().Error("handlePlayerJoinSync: failed to update discord nickname",
			"minecraft_name", mcName,
			"uuid", uuid,
			"discord_id", entry.DiscordID,
			"error", err,
		)
		return
	}

	logging.L().Info("handlePlayerJoinSync: updated discord nickname",
		"minecraft_name", mcName,
		"uuid", uuid,
		"discord_id", entry.DiscordID,
	)
}

func (a *App) sendWebhook(username, content, avatar string) {
	if a.Cfg.DiscordWebhookURL == "" {
		logging.L().Debug("sendWebhook: DiscordWebhookURL is empty, not sending webhook")
		return
	}
	flag := discordwebhook.MessageFlagSuppressNotifications
	if content == "" {
		logging.L().Debug("sendWebhook: webhook message is empty will not send.")
		return
	}
	msg := discordwebhook.Message{
		Content:   &content,
		Username:  &username,
		AvatarURL: &avatar,
		Flags:     &flag,
	}
	if err := discordwebhook.SendMessage(a.Cfg.DiscordWebhookURL, msg); err != nil {
		logging.L().Error("sendWebhook: webhook send fail", "error", err, "username", username, "content", content, "avatar", avatar)
	}
}
