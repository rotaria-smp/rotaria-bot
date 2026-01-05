package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) onGuildMemberRemove(_ *discordgo.Session, ev *discordgo.GuildMemberRemove) {
	log := logging.L().With("component", "discord", "module", "guild", "func", "onGuildMemberRemove")
	if ev.GuildID != a.Cfg.GuildID {
		return
	}
	ctx := context.Background()
	entry, _ := a.WLStore.GetByDiscord(ctx, ev.User.ID)
	if entry == nil {
		return
	}
	if a.Bridge.IsConnected() {

		if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("unwhitelist %s", entry.Username)); err != nil {
			log.Error("failed to unwhitelist user on bridge", "username", entry.Username, "error", err)
		}
	}
	_ = a.WLStore.Remove(ctx, ev.User.ID)
	log.Info("Removed whitelist for departing member", "username", entry.Username, "discord_id", ev.User.ID)
}
