package discord

import (
	"context"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) onGuildMemberRemove(_ *discordgo.Session, ev *discordgo.GuildMemberRemove) {
	if ev.GuildID != a.Cfg.GuildID {
		return
	}
	ctx := context.Background()
	entry, _ := a.WLStore.GetByDiscord(ctx, ev.User.ID)
	if entry == nil {
		return
	}
	if a.Bridge.IsConnected() {
		_, _ = a.Bridge.SendCommand(ctx, "unwhitelist "+entry.Username)
	}
	_ = a.WLStore.Remove(ctx, ev.User.ID)
	logging.L().Info("Removed whitelist for departing member", "username", entry.Username, "discord_id", ev.User.ID)
}
