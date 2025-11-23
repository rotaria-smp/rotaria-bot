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
}

func (a *App) handleWhitelistSubmit(i *discordgo.InteractionCreate) {
	username := modalValue(i, "mc_username")
	age := modalValue(i, "age")
	plan := modalValue(i, "plan")
	if username == "" || age == "" || plan == "" {
		a.reply(i, "Missing fields.", true)
		return
	}
	uuid, err := a.NameMC.UsernameToUUID(username)
	if err != nil {
		a.reply(i, fmt.Sprintf("Username %q not found.", username), true)
		return
	}

	embed := &discordgo.MessageEmbed{
		Title:       "Whitelist Request",
		Description: "New whitelist request.",
		Color:       0x3B82F6,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Applicant", Value: "<@" + i.Member.User.ID + ">", Inline: true},
			{Name: "Minecraft Username", Value: "`" + username + "`", Inline: true},
			{Name: "UUID", Value: "`" + uuid + "`", Inline: true},
			{Name: "Age", Value: age, Inline: true},
			{Name: "Plan", Value: plan},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			discordgo.Button{CustomID: "approve_" + username + "|" + i.Member.User.ID, Label: "Approve", Style: discordgo.SuccessButton},
			discordgo.Button{CustomID: "reject_" + username + "|" + i.Member.User.ID, Label: "Reject", Style: discordgo.DangerButton},
		}},
	}

	if a.Cfg.WhitelistRequestsChannelID != "" {
		_, err = a.Session.ChannelMessageSendComplex(a.Cfg.WhitelistRequestsChannelID,
			&discordgo.MessageSend{Embeds: []*discordgo.MessageEmbed{embed}, Components: components})
		if err != nil {
			logging.L().Error("whitelist submit send failed", "error", err)
		}
	}
	a.reply(i, fmt.Sprintf("Submitted request for %s.", username), true)
}

func (a *App) handleWhitelistDecision(i *discordgo.InteractionCreate) {
	custom := i.MessageComponentData().CustomID
	approved := strings.HasPrefix(custom, "approve_")
	var prefix string
	if approved {
		prefix = "approve_"
	} else if strings.HasPrefix(custom, "reject_") {
		prefix = "reject_"
	} else {
		return
	}
	payload := strings.TrimPrefix(custom, prefix)
	parts := strings.SplitN(payload, "|", 2)
	if len(parts) != 2 {
		a.reply(i, "Malformed decision.", true)
		return
	}
	username := parts[0]
	requesterID := parts[1]

	// Update embed
	if len(i.Message.Embeds) > 0 {
		cp := *i.Message.Embeds[0]
		line := fmt.Sprintf("📝 `%s` was **%s** by <@%s>. (Requested by <@%s>)",
			username, ternary(approved, "Approved", "Rejected"), i.Member.User.ID, requesterID)
		if strings.TrimSpace(cp.Description) == "" {
			cp.Description = line
		} else {
			cp.Description += "\n\n" + line
		}
		found := false
		for _, f := range cp.Fields {
			if f.Name == "Decision" {
				f.Value = ternary(approved, "Approved", "Rejected")
				found = true
				break
			}
		}
		if !found {
			cp.Fields = append(cp.Fields, &discordgo.MessageEmbedField{Name: "Decision", Value: ternary(approved, "Approved", "Rejected")})
		}
		cp.Timestamp = time.Now().UTC().Format(time.RFC3339)
		if approved {
			cp.Color = 0x22C55E
		} else {
			cp.Color = 0xEF4444
		}
		_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{Embeds: []*discordgo.MessageEmbed{&cp}},
		})
	} else {
		a.reply(i, "Missing embed.", true)
	}

	if !approved {
		return
	}

	ctx := context.Background()
	uuid, err := a.NameMC.UsernameToUUID(username)
	if err != nil {
		a.reply(i, fmt.Sprintf("UUID resolve failed for %q.", username), true)
		return
	}

	if _, err := a.Bridge.SendCommand(ctx, "whitelist add "+username); err != nil {
		a.reply(i, "Minecraft whitelist failed.", true)
		return
	}
	if err := a.Session.GuildMemberRoleAdd(a.Cfg.GuildID, requesterID, a.Cfg.MemberRoleID); err != nil {
		a.reply(i, "Role assign failed.", true)
		return
	}
	if err := a.WLStore.Add(ctx, requesterID, uuid, username); err != nil {
		a.reply(i, "DB insert failed.", true)
		return
	}
	_ = a.Session.GuildMemberNickname(i.GuildID, requesterID, username)
	if dm, err := a.Session.UserChannelCreate(requesterID); err == nil {
		_, _ = a.Session.ChannelMessageSend(dm.ID, fmt.Sprintf("✅ You are whitelisted: %s", username))
	}
}
