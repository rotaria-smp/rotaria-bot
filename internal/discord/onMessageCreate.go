package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	if a.Blacklist != nil && a.Blacklist.Contains(m.Content) {
		logging.L().Info("Blocked discord message", "author", m.Author.ID, "content", m.Content)
		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
		return
	}
	if m.ChannelID != a.Cfg.MinecraftDiscordMessengerChannelID {
		return
	}
	if !a.Bridge.IsConnected() {
		logging.L().Debug("Bridge not connected, skip relay")
		return
	}
	text := strings.TrimSpace(strings.ReplaceAll(m.Content, "\n", " "))
	if text == "" {
		return
	}
	payload := fmt.Sprintf("say [Discord] %s: %s", m.Author.Username, text)
	ctx := context.Background()
	if _, err := a.Bridge.SendCommand(ctx, payload); err != nil {
		logging.L().Warn("Relay failed", "error", err)
	}
}
