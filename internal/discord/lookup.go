package discord

import (
	"context"
	"fmt"
	"runtime"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
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
		defer func() {
			if r := recover(); r != nil {
				stack := make([]byte, 8192)
				n := runtime.Stack(stack, false)
				logging.L().Error("lookup panic", "recover", r, "stack", string(stack[:n]))
				safe := "internal error during lookup"
				if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &safe}); err != nil {
					logging.L().Error("lookup panic response edit failed", "error", err)
				}
			}
		}()

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
				logging.L().Error("lookup GetByDiscord failed", "discord_id", u.ID, "error", err)
				response = fmt.Sprintf("Lookup failed for <@%s>, please try again later.", u.ID)
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
						logging.L().Error("lookup GetByUsername failed", "minecraft_name", name, "error", dbErr)
						response = fmt.Sprintf("Resolve failed for `%s`, please try again later.", name)
					} else if entry == nil {
						response = fmt.Sprintf("`%s` not resolved & not whitelisted", name)
					} else {
						response = fmt.Sprintf("`%s` appears whitelisted (Discord <@%s>)", entry.Username, entry.DiscordID)
					}
				} else {
					entry, err := a.WLStore.GetByUUID(ctx, uuid)
					if err != nil {
						logging.L().Error("lookup GetByUUID failed", "minecraft_name", name, "uuid", uuid, "error", err)
						response = fmt.Sprintf("Lookup failed for `%s`, please try again later.", name)
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
