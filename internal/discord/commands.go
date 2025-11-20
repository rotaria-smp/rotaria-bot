package discord

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/discordwebhook"
	"github.com/rotaria-smp/rotaria-bot/internal/discord/blacklist"
	"github.com/rotaria-smp/rotaria-bot/internal/discord/namemc"
	"github.com/rotaria-smp/rotaria-bot/internal/mcbridge"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/config"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
	"github.com/rotaria-smp/rotaria-bot/internal/whitelist"
)

type App struct {
	Session   *discordgo.Session
	Cfg       config.Config
	Bridge    *mcbridge.Bridge
	WLStore   *whitelist.Store
	Blacklist *blacklist.List
	NameMC    *namemc.Client
}

func NewApp(sess *discordgo.Session, cfg config.Config, bridge *mcbridge.Bridge, wl *whitelist.Store, bl *blacklist.List) *App {
	return &App{
		Session:   sess,
		Cfg:       cfg,
		Bridge:    bridge,
		WLStore:   wl,
		Blacklist: bl,
		NameMC:    namemc.New(),
	}
}

func (a *App) Register() error {
	a.Session.AddHandler(a.onReady)
	a.Session.AddHandler(a.onMessageCreate)
	a.Session.AddHandler(a.onGuildMemberRemove)
	a.Session.AddHandler(a.onInteraction)
	commands := []*discordgo.ApplicationCommand{
		{Name: "list", Description: "List online players"},
		{Name: "whitelist", Description: "Begin whitelist application"},
		{Name: "report", Description: "Report an issue"},
	}
	for _, c := range commands {
		if _, err := a.Session.ApplicationCommandCreate(a.Session.State.User.ID, "", c); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) onReady(s *discordgo.Session, r *discordgo.Ready) {
	logging.L().Info("Ready", "user", fmt.Sprintf("%s#%s", r.User.Username, r.User.Discriminator))
}

func (a *App) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if strings.Contains(m.Content, "\"everyone") {
		_ = s.MessageReactionAdd(m.ChannelID, m.ID, "❌")
	}

	if m.Author.Bot {
		return
	}

	logging.L().Debug("onMessageCreate: received message", "channel", m.ChannelID, "author", m.Author.ID, "content", m.Content)

	// Blacklist check
	if a.Blacklist != nil && a.Blacklist.Contains(m.Content) {
		logging.L().Info("Blocked message from user (blacklist hit)", "message", m.Content, "user", m.Author.ID)
		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
		return
	}

	if m.ChannelID != a.Cfg.MinecraftDiscordMessengerChannelID {
		return
	}

	if !a.Bridge.IsConnected() {
		logging.L().Debug("minecraft not connected; cannot relay discord message")
		return
	}

	text := strings.TrimSpace(m.Content)
	if text == "" {
		return
	}
	text = strings.ReplaceAll(text, "\n", " ")

	ctx := context.Background()
	payload := fmt.Sprintf("say [Discord] %s: %s", m.Author.Username, text)
	logging.L().Debug("Relaying to Minecraft via bridge", "payload", payload)

	out, err := a.Bridge.SendCommand(ctx, payload)
	if err != nil {
		logging.L().Warn("relay to minecraft failed", "error", err)
	} else {
		logging.L().Debug("relay to minecraft ok", "response", out)
	}
}

func (a *App) onGuildMemberRemove(s *discordgo.Session, ev *discordgo.GuildMemberRemove) {
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
	logging.L().Info("Removed whitelist for user", "username", entry.Username, "discord_id", ev.User.ID)
}

func (a *App) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		switch i.ApplicationCommandData().Name {
		case "list":
			// Immediate deferred ack (ephemeral)
			err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{
					Flags: discordgo.MessageFlagsEphemeral,
				},
			})
			if err != nil {
				logging.L().Warn("defer list respond failed", "error", err)
				return
			}
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
				defer cancel()
				out, err := a.Bridge.SendCommand(ctx, "list")
				if err != nil {
					out = "Error: " + err.Error()
				}
				_, e2 := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{
					Content: &out,
				})
				if e2 != nil {
					logging.L().Warn("edit list reply failed", "error", e2)
				}
			}()
		case "whitelist":
			a.openWhitelistModal(i)
		case "report":
			a.openReportModal(i)
		}
	case discordgo.InteractionModalSubmit:
		cid := i.ModalSubmitData().CustomID
		if cid == "whitelist_modal" {
			a.handleWhitelistSubmit(i)
		} else if cid == "report_modal" {
			a.handleReportSubmit(i)
		} else if strings.HasPrefix(cid, "report_action_modal|") {
			a.handleReportActionModal(i)
		}
	case discordgo.InteractionMessageComponent:
		c := i.MessageComponentData().CustomID
		if c == "request_whitelist" {
			a.openWhitelistModal(i)
		} else if strings.HasPrefix(c, "report_resolve_") || strings.HasPrefix(c, "report_dismiss_") {
			a.openReportActionModal(i)
		} else if strings.HasPrefix(c, "approve_") || strings.HasPrefix(c, "reject_") {
			a.handleWhitelistDecision(i)
		}
	}
}

func (a *App) reply(i *discordgo.InteractionCreate, msg string, eph bool) {
	flags := discordgo.MessageFlags(0)
	if eph {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: msg,
			Flags:   flags,
		},
	})
}

func (a *App) openWhitelistModal(i *discordgo.InteractionCreate) {
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "whitelist_modal",
			Title:    "Enter Your Minecraft Username",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "mc_username",
							Label:       "Minecraft Username",
							Style:       discordgo.TextInputShort,
							Placeholder: "e.g. Notch",
							Required:    true,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "age",
							Label:       "Whats your age",
							Style:       discordgo.TextInputShort,
							Placeholder: "16+",
							Required:    true,
						},
					},
				},
				discordgo.ActionsRow{
					Components: []discordgo.MessageComponent{
						discordgo.TextInput{
							CustomID:    "plan",
							Label:       "what do you plan on doing on the server?",
							Style:       discordgo.TextInputShort,
							Placeholder: "build, economy, towns, etc",
							Required:    true,
						},
					},
				},
			},
		},
	})
}

func (a *App) handleWhitelistSubmit(i *discordgo.InteractionCreate) {
	logging.L().Debug("handleWhitelistSubmit: guild and user", "guild", i.GuildID, "user", i.Member.User.ID)

	username := modalValue(i, "mc_username")
	age := modalValue(i, "age")
	plan := modalValue(i, "plan")
	logging.L().Debug("handleWhitelistSubmit: received form values", "username", username, "age", age, "plan", plan)

	if username == "" || age == "" || plan == "" {
		a.reply(i, "Missing required fields.", true)
		return
	}

	uuid, err := a.NameMC.UsernameToUUID(username)
	if err != nil {
		logging.L().Debug("handleWhitelistSubmit: UsernameToUUID failed", "username", username, "error", err)
		a.reply(i, fmt.Sprintf("Seems like username %q does not exist.", username), true)
		return
	}

	logging.L().Debug("handleWhitelistSubmit: resolved username to UUID", "username", username, "uuid", uuid)

	embed := &discordgo.MessageEmbed{
		Title:       "Whitelist Request",
		Description: "A new whitelist request has been submitted.",
		Color:       0x3B82F6,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Applicant", Value: "<@" + i.Member.User.ID + ">", Inline: true},
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
					CustomID: "approve_" + username + "|" + i.Member.User.ID,
					Label:    "Approve",
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					CustomID: "reject_" + username + "|" + i.Member.User.ID,
					Label:    "Reject",
					Style:    discordgo.DangerButton,
				},
			},
		},
	}

	if a.Cfg.WhitelistRequestsChannelID == "" {
		logging.L().Debug("handleWhitelistSubmit: WhitelistRequestsChannelID is empty; not sending embed")
	} else {
		logging.L().Debug("handleWhitelistSubmit: sending embed to channel", "channel", a.Cfg.WhitelistRequestsChannelID)
		_, err := a.Session.ChannelMessageSendComplex(
			a.Cfg.WhitelistRequestsChannelID,
			&discordgo.MessageSend{
				Embeds:     []*discordgo.MessageEmbed{embed},
				Components: components,
			},
		)
		if err != nil {
			logging.L().Error("handleWhitelistSubmit: ChannelMessageSendComplex failed", "error", err)
		}
	}

	a.reply(i, fmt.Sprintf("Submitted whitelist request for %s. Staff will review soon.", username), true)
}

func (a *App) handleWhitelistDecision(i *discordgo.InteractionCreate) {
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
			i.Member.User.ID,
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
			logging.L().Error("handleWhitelistDecision: UsernameToUUID failed", "username", username, "error", err)
			a.reply(i, fmt.Sprintf("Could not resolve username %q or UUID endpoint is down.", username), true)
			return
		}
		_ = a.WLStore.Add(ctx, requesterID, uuid, username)
		if a.Bridge.IsConnected() {
			_, _ = a.Bridge.SendCommand(ctx, "whitelist add "+username)
		}
		if dm, err := a.Session.UserChannelCreate(requesterID); err == nil {
			_, _ = a.Session.ChannelMessageSend(dm.ID, fmt.Sprintf("✅ You have been whitelisted on Rotaria!\nWelcome to Rotaria, `%s` 🎉", username))
		}
	}
}

func ternary[T any](cond bool, a T, b T) T {
	if cond {
		return a
	}
	return b
}

func (a *App) openReportModal(i *discordgo.InteractionCreate) {
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "report_modal",
			Title:    "Report Issue",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_type", Label: "Report Type (player / bug / other)", Style: discordgo.TextInputShort, Required: true, MaxLength: 16},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "reported_username", Label: "Reported Player (if type=player)", Style: discordgo.TextInputShort, Required: false, MaxLength: 64},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_reason", Label: "Details - what happened?", Style: discordgo.TextInputParagraph, Required: true, MaxLength: 1000},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_evidence", Label: "Evidence (links, optional)", Style: discordgo.TextInputShort, Required: false, MaxLength: 200, Placeholder: "Screenshot / video links"},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_context", Label: "Context (where/when, optional)", Style: discordgo.TextInputShort, Required: false, MaxLength: 200},
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

// Open moderator action modal for resolve/dismiss
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

// Handle moderator action modal submission
func (a *App) handleReportActionModal(i *discordgo.InteractionCreate) {
	parts := strings.SplitN(i.ModalSubmitData().CustomID, "|", 3)
	if len(parts) != 3 {
		return
	}
	action := parts[1]
	orig := parts[2]
	note := modalValue(i, "moderator_note")
	if note == "" {
		note = "(no note)"
	}
	msg := i.Message
	if msg == nil || len(msg.Embeds) == 0 {
		a.reply(i, "Original report message missing.", true)
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
	embeds := []*discordgo.MessageEmbed{&cp}
	components := []discordgo.MessageComponent{}
	_, _ = a.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    i.ChannelID,
		ID:         msg.ID,
		Embeds:     &embeds,
		Components: &components,
	})
	a.reply(i, "Report updated.", true)
	_ = orig
}

func modalValue(i *discordgo.InteractionCreate, id string) string {
	for _, row := range i.ModalSubmitData().Components {
		if ar, ok := row.(*discordgo.ActionsRow); ok {
			for _, c := range ar.Components {
				if ti, ok := c.(*discordgo.TextInput); ok && ti.CustomID == id {
					return strings.TrimSpace(ti.Value)
				}
			}
		}
	}
	return ""
}

var chatLineRe = regexp.MustCompile(`^<([^>]+)>[ ]?(.*)$`)
var atEveryone = regexp.MustCompile(`@everyone`)

func (a *App) HandleMCEvent(topic, body string) {
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}

	if topic == "status" {
		body = strings.TrimSpace(body)
		if body == "" {
			return
		}

		if err := a.Session.UpdateGameStatus(0, body); err != nil {
			logging.L().Error("HandleMCEvent: failed to update presence", "error", err)
		} else {
			logging.L().Debug("HandleMCEvent: updated presence", "presence", body)
		}
		return
	}

	if topic == "join" || topic == "leave" || topic == "lifecycle" {
		a.sendWebhook("Rotaria", body, "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp")
		return
	}

	if topic == "chat" {
		username := "Server"
		msg := body
		if m := chatLineRe.FindStringSubmatch(body); m != nil {
			username = m[1]
			msg = m[2]
		}

		msg = atEveryone.ReplaceAllString(msg, "\"everyone")

		if a.Blacklist != nil && a.Blacklist.Contains(msg) {
			logging.L().Info("Blocked message from user (blacklist hit)", "message", msg, "user", username)
			if a.Bridge.IsConnected() {
				ctx := context.Background()
				_, _ = a.Bridge.SendCommand(ctx, "kick "+username)
			}
			return
		}

		if strings.TrimSpace(msg) == "" {
			return
		}

		a.sendWebhook(username, msg, "https://minotar.net/avatar/"+username+"/128.png")
	}
}

func (a *App) sendWebhook(username, content, avatar string) {
	if a.Cfg.DiscordWebhookURL == "" {
		logging.L().Debug("sendWebhook: DiscordWebhookURL is empty, not sending webhook")
		return
	}
	flag := discordwebhook.MessageFlagSuppressNotifications
	if content == "" {
		logging.L().Debug("sendWebhook: webhook message is empty will not send.")
		return
	}
	msg := discordwebhook.Message{
		Content:   &content,
		Username:  &username,
		AvatarURL: &avatar,
		Flags:     &flag,
	}
	if err := discordwebhook.SendMessage(a.Cfg.DiscordWebhookURL, msg); err != nil {
		logging.L().Error("sendWebhook: webhook send fail", "error", err)
	}
}
