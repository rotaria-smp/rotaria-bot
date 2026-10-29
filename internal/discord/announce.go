package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func newDowntimeCommand(perm int64) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:                     "downtime",
		Description:              "Announce upcoming server downtime",
		DefaultMemberPermissions: &perm,
		Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionString, Name: "details", Description: "Reason / expected duration"},
		},
	}
}

func newOnlineCommand(perm int64) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:                     "online",
		Description:              "Announce the server is back online",
		DefaultMemberPermissions: &perm,
		Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
	}
}

func (a *App) handleDowntime(i *discordgo.InteractionCreate) {
	desc := "The server will be down for a while."
	if opts := i.ApplicationCommandData().Options; len(opts) > 0 {
		desc += "\n\n" + opts[0].StringValue()
	}
	a.announce(i, &discordgo.MessageEmbed{Title: "Server downtime", Description: desc, Color: 0xe74c3c})
}

func (a *App) handleOnline(i *discordgo.InteractionCreate) {
	a.announce(i, &discordgo.MessageEmbed{Title: "Server online", Description: "The server is back online. See you in game!", Color: 0x2ecc71})
}

// announce posts the embed to the updates channel and pings the member role.
func (a *App) announce(i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	log := logging.L().With("component", "discord", "module", "announce", "func", "announce")
	if a.Cfg.UpdatesChannelID == "" {
		a.reply(i, "UPDATES_CHANNEL_ID is not configured.", true)
		return
	}
	_, err := a.Session.ChannelMessageSendComplex(a.Cfg.UpdatesChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("<@&%s>", a.Cfg.MemberRoleID),
		Embeds:          []*discordgo.MessageEmbed{embed},
		AllowedMentions: &discordgo.MessageAllowedMentions{Roles: []string{a.Cfg.MemberRoleID}},
	})
	if err != nil {
		log.Error("send failed", "err", err)
		a.reply(i, "Failed to post: "+err.Error(), true)
		return
	}
	a.reply(i, "Posted.", true)
}
