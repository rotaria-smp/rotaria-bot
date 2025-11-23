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
	chatLineRe  = regexp.MustCompile(`^<([^>]+)>[ ]?(.*)$`)
	joinLineRe  = regexp.MustCompile(`^\*\*([A-Za-z0-9_]+)\*\* joined the server\.$`)
	atEveryone  = regexp.MustCompile(`@everyone`)
	nameExtract = regexp.MustCompile(`([A-Za-z0-9_]+)$`)
)

func (a *App) HandleMCEvent(topic, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}

	if topic == "status" {
		now := time.Now()
		if now.Sub(a.lastStatusUpdate) < time.Minute {
			return
		}
		if err := a.Session.UpdateGameStatus(0, body); err == nil {
			a.lastStatusUpdate = now
		}
		return
	}

	if topic == "join" {
		if m := joinLineRe.FindStringSubmatch(body); m != nil {
			go a.handlePlayerJoinSync(m[1])
		}
		a.sendWebhook("Rotaria", body, rotariaAvatar())
		return
	}

	if topic == "leave" || topic == "lifecycle" {
		a.sendWebhook("Rotaria", body, rotariaAvatar())
		return
	}

	if topic == "chat" {
		msg := body
		fullUsername := "server"
		minecraftName := "server"

		if m := chatLineRe.FindStringSubmatch(body); m != nil {
			fullUsername = m[1]
			if n := nameExtract.FindStringSubmatch(fullUsername); len(n) > 1 {
				minecraftName = n[1]
			} else {
				minecraftName = fullUsername
			}
			msg = m[2]
		}

		msg = atEveryone.ReplaceAllString(msg, "\"everyone")
		if a.Blacklist != nil && a.Blacklist.Contains(msg) {
			if a.Bridge.IsConnected() {
				ctx := context.Background()
				_, _ = a.Bridge.SendCommand(ctx, fmt.Sprintf("kick %s", minecraftName))
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
		return
	}
	entry, err := a.WLStore.GetByUUID(ctx, uuid)
	if err != nil || entry == nil {
		return
	}
	if entry.Username != mcName {
		_ = a.WLStore.UpdateUser(ctx, entry.DiscordID, uuid, mcName)
	}
	_ = a.Session.GuildMemberNickname(a.Cfg.GuildID, entry.DiscordID, mcName)
}

func (a *App) sendWebhook(username, content, avatar string) {
	if a.Cfg.DiscordWebhookURL == "" || strings.TrimSpace(content) == "" {
		return
	}
	flag := discordwebhook.MessageFlagSuppressNotifications
	msg := discordwebhook.Message{
		Content:   &content,
		Username:  &username,
		AvatarURL: &avatar,
		Flags:     &flag,
	}
	if err := discordwebhook.SendMessage(a.Cfg.DiscordWebhookURL, msg); err != nil {
		logging.L().Error("webhook send fail", "error", err, "username", username)
	}
}

func rotariaAvatar() string {
	return "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp"
}
