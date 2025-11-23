package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) onReady(_ *discordgo.Session, r *discordgo.Ready) {
	logging.L().Info("Ready", "user", fmt.Sprintf("%s#%s", r.User.Username, r.User.Discriminator))
}
