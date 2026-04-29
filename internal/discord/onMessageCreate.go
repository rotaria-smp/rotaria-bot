package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	log := logging.L().With("component", "discord", "module", "messages", "func", "onMessageCreate")
	if m.Author.Bot {
		return
	}

	log.Debug("received message", "channel", m.ChannelID, "author", m.Author.ID, "content", m.Content)
	
	// Blacklist check
	if a.Blacklist != nil && a.Blacklist.Contains(m.Content) {
		log.Info("blocked (blacklist hit)", "message", m.Content, "user", m.Author.ID)
		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
		return
	}

	if m.ChannelID != a.Cfg.MinecraftDiscordMessengerChannelID {
		return
	}

	if !a.Bridge.IsConnected() {
		log.Debug("minecraft not connected; cannot relay discord message")
		return
	}

	text := strings.TrimSpace(m.Content)
	if text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\n", " ")
	displayName := m.Author.Username

    if m.Member != nil && m.Member.Nick != "" {
        displayName = m.Member.Nick
    }
	ctx := context.Background()
	payload := fmt.Sprintf("say [Discord] %s: %s", displayName, text)
	log.Debug("relaying to Minecraft via bridge", "payload", payload)

	out, err := a.Bridge.SendCommand(ctx, payload)
	if err != nil {
		log.Warn("relay to minecraft failed", "error", err)
	} else {
		log.Debug("relay to minecraft ok", "response", out)
	}
}
