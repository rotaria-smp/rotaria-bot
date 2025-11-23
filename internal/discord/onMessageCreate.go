package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if strings.Contains(m.Content, "everyone") {
		_ = s.MessageReactionAdd(m.ChannelID, m.ID, "❌")
	}

	if m.Author.Bot {
		return
	}

	logging.L().Debug("onMessageCreate: received message", "channel", m.ChannelID, "author", m.Author.ID, "content", m.Content)

	// Blacklist check
	if a.Blacklist != nil && a.Blacklist.Contains(m.Content) {
		logging.L().Info("Blocked message from user (blacklist hit)", "message", m.Content, "user", m.Author.ID)
		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
		return
	}

	if m.ChannelID != a.Cfg.MinecraftDiscordMessengerChannelID {
		return
	}

	if !a.Bridge.IsConnected() {
		logging.L().Debug("minecraft not connected; cannot relay discord message")
		return
	}

	text := strings.TrimSpace(m.Content)
	if text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\n", " ")

	ctx := context.Background()
	payload := fmt.Sprintf("say [Discord] %s: %s", m.Author.Username, text)
	logging.L().Debug("Relaying to Minecraft via bridge", "payload", payload)

	out, err := a.Bridge.SendCommand(ctx, payload)
	if err != nil {
		logging.L().Warn("relay to minecraft failed", "error", err)
	} else {
		logging.L().Debug("relay to minecraft ok", "response", out)
	}
}
