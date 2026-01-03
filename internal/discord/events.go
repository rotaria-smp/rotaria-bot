package discord

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

var (
	chatLineRe = regexp.MustCompile(`^<([^>]+)>[ ]?(.*)$`)
	joinLineRe = regexp.MustCompile(`^\*\*([A-Za-z0-9_]+)\*\* joined the server\.$`)
	atEveryone = regexp.MustCompile(`(?i)@(everyone|here)\b`)
	nameRe     = regexp.MustCompile(`([A-Za-z0-9_]+)$`)
)

var rotariaAvatarUrl string = "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp"

func (a *App) HandleMCEvent(topic, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}

	if topic == "status" {
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

	if topic == "join" {
		logging.L().Debug("Player joined", "message", body)

		if m := joinLineRe.FindStringSubmatch(body); m != nil {
			mcName := m[1] // e.g. "limp4n__"
			logging.L().Debug("Parsed join username", "minecraft_name", mcName)

			go a.handlePlayerJoinSync(mcName)
		}

		a.WebhookQueue.Enqueue("Rotaria", body, rotariaAvatarUrl)
		return
	}

	if topic == "leave" || topic == "lifecycle" {
		a.WebhookQueue.Enqueue("Rotaria", body, rotariaAvatarUrl)
		return
	}

	if topic == "chat" {
		msg := body
		fullUsername := "server"
		minecraftName := "server"

		if m := chatLineRe.FindStringSubmatch(body); m != nil {
			fullUsername = m[1]

			if n := nameRe.FindStringSubmatch(fullUsername); len(n) > 1 {
				minecraftName = n[1]
			} else {
				minecraftName = fullUsername
			}

			msg = m[2]
		}

		msg = atEveryone.ReplaceAllString(msg, `$1`)

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

		avatarURL := fmt.Sprintf("https://minotar.net/avatar/%s/128.png", minecraftName)
		a.WebhookQueue.Enqueue(fullUsername, msg, avatarURL)
	}
}

func (a *App) handlePlayerJoinSync(mcName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uuid, err := a.NameMC.UsernameToUUID(mcName)
	if err != nil {
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