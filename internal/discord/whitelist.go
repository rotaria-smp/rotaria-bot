package discord

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) openWhitelistModal(i *discordgo.InteractionCreate) {
	log := logging.L().With("component", "discord", "module", "whitelist", "func", "openWhitelistModal")
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "whitelist_modal",
			Title:    "Whitelist Application",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{CustomID: "mc_username", Label: "Minecraft Username", Style: discordgo.TextInputShort, Required: true},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{CustomID: "age", Label: "Age", Style: discordgo.TextInputShort, Required: true},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					discordgo.TextInput{CustomID: "plan", Label: "What will you do?", Style: discordgo.TextInputShort, Required: true},
				}},
			},
		},
	})
	log.Debug("opened whitelist modal", "user", actorID(i))
}

func (a *App) handleWhitelistSubmit(i *discordgo.InteractionCreate) {
	log := logging.L().With("component", "discord", "module", "whitelist", "func", "handleWhitelistSubmit")
	log.Debug("guild and user", "guild", i.GuildID, "user", actorID(i))

	username := modalValue(i, "mc_username")
	age := modalValue(i, "age")
	plan := modalValue(i, "plan")
	log.Debug("received form values", "username", username, "age", age, "plan", plan)

	if username == "" || age == "" || plan == "" {
		a.reply(i, "Missing required fields.", true)
		return
	}

	uuid, err := a.NameMC.UsernameToUUID(username)
	if err != nil {
		log.Warn("UsernameToUUID failed", "username", username, "error", err)
		a.reply(i, fmt.Sprintf("Seems like username %q does not exist.", username), true)
		return
	}

	log.Debug("resolved username to UUID", "username", username, "uuid", uuid)

	embed := &discordgo.MessageEmbed{
		Title:       "Whitelist Request",
		Description: "A new whitelist request has been submitted.",
		Color:       0x3B82F6,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Applicant", Value: "<@" + actorID(i) + ">", Inline: true},
			{Name: "Minecraft Username", Value: "`" + username + "`", Inline: true},
			{Name: "UUID", Value: "`" + uuid + "`", Inline: true},
			{Name: "Age", Value: age, Inline: true},
			{Name: "Plan", Value: plan},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Footer:    &discordgo.MessageEmbedFooter{Text: "Rotaria Whitelist"},
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					CustomID: "approve_" + username + "|" + actorID(i),
					Label:    "Approve",
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					CustomID: "reject_" + username + "|" + actorID(i),
					Label:    "Reject",
					Style:    discordgo.DangerButton,
				},
			},
		},
	}

	if a.Cfg.WhitelistRequestsChannelID == "" {
		log.Debug("WhitelistRequestsChannelID is empty; not sending embed")
	} else {
		log.Debug("sending embed to channel", "channel", a.Cfg.WhitelistRequestsChannelID)
		_, err := a.Session.ChannelMessageSendComplex(
			a.Cfg.WhitelistRequestsChannelID,
			&discordgo.MessageSend{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		)
		if err != nil {
			log.Error("handleWhitelistSubmit: ChannelMessageSendComplex failed", "error", err)
		}
	}

	a.reply(i, fmt.Sprintf("Submitted whitelist request for %s. Staff will review soon.", username), true)
}

func (a *App) handleWhitelistDecision(i *discordgo.InteractionCreate) {
	log := logging.L().With("component", "discord", "module", "whitelist", "func", "handleWhitelistDecision")
	if !a.Bridge.IsConnected() {
		// TODO: rejections would still be able to go through as they do not need server connection
		a.reply(i, "Minecraft server is not connected; cannot process whitelist decisions right now.", true)
		return
	}
	custom := i.MessageComponentData().CustomID
	approved := false
	var prefix string
	if strings.HasPrefix(custom, "approve_") {
		approved = true
		prefix = "approve_"
	} else if strings.HasPrefix(custom, "reject_") {
		prefix = "reject_"
	} else {
		return
	}

	payload := strings.TrimPrefix(custom, prefix)
	parts := strings.SplitN(payload, "|", 2)
	if len(parts) != 2 {
		a.reply(i, "Malformed decision ID.", true)
		return
	}
	username := parts[0]
	requesterID := parts[1]

	if len(i.Message.Embeds) > 0 {
		cp := *i.Message.Embeds[0]

		statusLine := fmt.Sprintf(
			"📝 Request for `%s` was **%s** by <@%s>. (Requested by: <@%s>)",
			username,
			ternary(approved, "Approved", "Rejected"),
			actorID(i),
			requesterID,
		)

		if strings.TrimSpace(cp.Description) == "" {
			cp.Description = statusLine
		} else {
			cp.Description += "\n\n" + statusLine
		}

		found := false
		for _, f := range cp.Fields {
			if strings.EqualFold(f.Name, "Decision") {
				f.Value = ternary(approved, "Approved", "Rejected")
				found = true
				break
			}
		}
		if !found {
			cp.Fields = append(cp.Fields, &discordgo.MessageEmbedField{
				Name:  "Decision",
				Value: ternary(approved, "Approved", "Rejected"),
			})
		}
		cp.Timestamp = time.Now().UTC().Format(time.RFC3339)
		if approved {
			cp.Color = 0x22C55E
		} else {
			cp.Color = 0xEF4444
		}

		_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Embeds:     []*discordgo.MessageEmbed{&cp},
				Components: []discordgo.MessageComponent{},
			},
		})
	} else {
		a.reply(i, "Missing embed.", true)

	}

	if approved {
		ctx := context.Background()
		uuid, err := a.NameMC.UsernameToUUID(username)
		if err != nil {
			log.Error("handleWhitelistDecision: UsernameToUUID failed", "username", username, "error", err)
			a.reply(i, fmt.Sprintf("Could not resolve username %q or UUID endpoint is down.", username), true)
			return
		}

		/*
			1. Try to whitelist user on minecraft, exit if failed
			2. Try to add member role, exit if failed
			3. Try to save entry to database, exit if failed
			4. Try to rename guild user to minecraft username, Exit if failed
		*/
		if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("whitelist add %s", username)); err != nil {
			log.Error("Failed to send whitelist add command to bridge", "error", err)
			a.reply(i, fmt.Sprintf("Failed to send whitelist command to minecraft server, please try again or try contacting @<@%s>", "322015089529978880"), true)
			return
		}
		if err := a.Session.GuildMemberRoleAdd(a.Cfg.GuildID, requesterID, a.Cfg.MemberRoleID); err != nil {
			log.Error("Failed to assign member role during whitelist decision", "error", err)
			a.reply(i, fmt.Sprintf("Failed to assign member role, please try again or try contacting <@%s>", "322015089529978880"), true)
			return
		}
		if err := a.WLStore.Add(ctx, requesterID, uuid, username); err != nil {
			log.Error("Failed to add whitelist entry to database", "error", err)
			a.reply(i, fmt.Sprintf("Failed to assign member role, please try again or try contacting <@%s>", "322015089529978880"), true)
			return
		}
		if err = a.Session.GuildMemberNickname(i.GuildID, requesterID, username); err != nil {
			log.Error("Failed to set guild member nickname during whitelist decision", "error", err)
			a.reply(i, fmt.Sprintf("Failed to set your nickname, please try again or try contacting <@%s>", "322015089529978880"), true)
		}

		if dm, err := a.Session.UserChannelCreate(requesterID); err == nil {
			_, _ = a.Session.ChannelMessageSend(dm.ID, fmt.Sprintf("✅ You have been whitelisted on Rotaria! Welcome to Rotaria, `%s` 🎉", username))
		}

	}
}
