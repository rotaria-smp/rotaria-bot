package discord

import (
	"fmt"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
)

func (a *App) openReportModal(i *discordgo.InteractionCreate) {
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "report_modal",
			Title:    "Report Issue",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_type", Label: "Type (player / bug / other)", Style: discordgo.TextInputShort, Required: true, MaxLength: 16},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "reported_username", Label: "Player (if type=player)", Style: discordgo.TextInputShort, Required: false, MaxLength: 64},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_reason", Label: "Details", Style: discordgo.TextInputParagraph, Required: true, MaxLength: 1000},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_evidence", Label: "Evidence (links)", Style: discordgo.TextInputShort, Required: false, MaxLength: 200},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_context", Label: "Context (optional)", Style: discordgo.TextInputShort, Required: false, MaxLength: 200},
				}},
			},
		},
	})
}

func (a *App) handleReportSubmit(i *discordgo.InteractionCreate) {
	t := strings.ToLower(strings.TrimSpace(modalValue(i, "report_type")))
	player := strings.TrimSpace(modalValue(i, "reported_username"))
	reason := strings.TrimSpace(modalValue(i, "report_reason"))
	evidence := strings.TrimSpace(modalValue(i, "report_evidence"))
	context := strings.TrimSpace(modalValue(i, "report_context"))

	if t == "player" && player == "" {
		a.reply(i, "Player report requires a username.", true)
		return
	}

	fields := []*discordgo.MessageEmbedField{
		{Name: "Reporter", Value: "<@" + i.Member.User.ID + ">", Inline: true},
		{Name: "Type", Value: strings.Title(t), Inline: true},
	}
	if player != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Reported Player", Value: "`" + player + "`", Inline: true})
	}
	if reason != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Details", Value: reason})
	}
	if evidence != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Evidence", Value: evidence})
	}
	if context != "" {
		fields = append(fields, &discordgo.MessageEmbedField{Name: "Context", Value: context})
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("New %s Report", strings.Title(t)),
		Description: "A new report has been filed.",
		Color:       0xF44336,
		Fields:      fields,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Footer:      &discordgo.MessageEmbedFooter{Text: "Rotaria Moderation"},
	}

	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			&discordgo.Button{CustomID: "report_resolve_" + player + "|" + i.Member.User.ID, Label: "Resolve", Style: discordgo.SuccessButton},
			&discordgo.Button{CustomID: "report_dismiss_" + player + "|" + i.Member.User.ID, Label: "Dismiss", Style: discordgo.DangerButton},
		}},
	}

	if a.Cfg.ReportChannelID != "" {
		_, _ = a.Session.ChannelMessageSendComplex(a.Cfg.ReportChannelID, &discordgo.MessageSend{
			Embeds:     []*discordgo.MessageEmbed{embed},
			Components: components,
		})
	}
	a.reply(i, "Report submitted.", true)
}

func (a *App) openReportActionModal(i *discordgo.InteractionCreate) {
	cid := i.MessageComponentData().CustomID
	action := "resolve"
	if strings.HasPrefix(cid, "report_dismiss_") {
		action = "dismiss"
	}
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "report_action_modal|" + action + "|" + cid,
			Title:    strings.Title(action) + " Report",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "moderator_note", Label: "Moderator Note", Style: discordgo.TextInputParagraph, Required: true, MaxLength: 1000},
				}},
			},
		},
	})
}

func (a *App) handleReportActionModal(i *discordgo.InteractionCreate) {
	parts := strings.SplitN(i.ModalSubmitData().CustomID, "|", 3)
	if len(parts) != 3 {
		return
	}
	action := parts[1]
	note := modalValue(i, "moderator_note")
	if note == "" {
		note = "(no note)"
	}
	msg := i.Message
	if msg == nil || len(msg.Embeds) == 0 {
		a.reply(i, "Original report missing.", true)
		return
	}
	cp := *msg.Embeds[0]
	label := "Resolved"
	color := 0x22C55E
	if action == "dismiss" {
		label = "Dismissed"
		color = 0xEF4444
	}
	line := fmt.Sprintf("📝 %s by <@%s>. Note: %s", label, i.Member.User.ID, note)
	if strings.TrimSpace(cp.Description) == "" {
		cp.Description = line
	} else {
		cp.Description += "\n\n" + line
	}
	cp.Color = color
	cp.Timestamp = time.Now().UTC().Format(time.RFC3339)
	_, _ = a.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel: i.ChannelID,
		ID:      msg.ID,
		Embeds:  &[]*discordgo.MessageEmbed{&cp},
	})
	a.reply(i, "Report updated.", true)
}
