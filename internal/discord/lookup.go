package discord

import (
	"context"
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

func newLookupCommand(perm int64) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:                     "lookup",
		Description:              "Lookup Minecraft username (admin only)",
		DefaultMemberPermissions: &perm,
		Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "discord_user", Description: "Discord user", Required: false},
			{Type: discordgo.ApplicationCommandOptionString, Name: "minecraft_name", Description: "Minecraft username", Required: false},
		},
	}
}

func (a *App) handleLookup(i *discordgo.InteractionCreate) {
	s := a.Session
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		return
	}
	go func() {
		ctx := context.Background()
		optMap := map[string]*discordgo.ApplicationCommandInteractionDataOption{}
		for _, o := range i.ApplicationCommandData().Options {
			optMap[o.Name] = o
		}
		var response string
		if o, ok := optMap["discord_user"]; ok {
			u := o.UserValue(s)
			entry, err := a.WLStore.GetByDiscord(ctx, u.ID)
			if err != nil {
				response = fmt.Sprintf("Lookup failed for <@%s>: %v", u.ID, err)
			} else if entry == nil {
				response = fmt.Sprintf("<@%s> not whitelisted", u.ID)
			} else {
				response = fmt.Sprintf("<@%s> => `%s`", u.ID, entry.Username)
			}
		} else if o, ok := optMap["minecraft_name"]; ok {
			name := strings.TrimSpace(o.StringValue())
			if name == "" {
				response = "Minecraft name cannot be empty"
			} else {
				uuid, err := a.NameMC.UsernameToUUID(name)
				if err != nil {
					entry, dbErr := a.WLStore.GetByUsername(ctx, name)
					if dbErr != nil {
						response = fmt.Sprintf("Resolve failed for `%s`: %v", name, dbErr)
					} else if entry == nil {
						response = fmt.Sprintf("`%s` not resolved & not whitelisted", name)
					} else {
						response = fmt.Sprintf("`%s` appears whitelisted (Discord <@%s>)", entry.Username, entry.DiscordID)
					}
				} else {
					entry, err := a.WLStore.GetByUUID(ctx, uuid)
					if err != nil {
						response = fmt.Sprintf("Lookup failed for `%s`: %v", name, err)
					} else if entry == nil {
						response = fmt.Sprintf("`%s` (UUID %s) not in whitelist DB", name, uuid)
					} else {
						response = fmt.Sprintf("`%s` is whitelisted (Discord <@%s>)", entry.Username, entry.DiscordID)
					}
				}
			}
		} else {
			response = "Provide one option: discord_user or minecraft_name"
		}
		_, _ = s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &response})
	}()
}
