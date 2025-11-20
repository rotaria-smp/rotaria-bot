package discord

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/discordwebhook"
	"github.com/rotaria-smp/rotaria-bot/internal/discord/blacklist"
	"github.com/rotaria-smp/rotaria-bot/internal/discord/namemc"
	"github.com/rotaria-smp/rotaria-bot/internal/mcbridge"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/config"
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
	log.Printf("Ready as %s#%s", r.User.Username, r.User.Discriminator)
}

func (a *App) onMessageCreate(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author.Bot {
		return
	}
	// Webhook forward + blacklist check
	if a.Blacklist != nil && a.Blacklist.Contains(m.Content) {
		log.Printf("Blocked message from %s", m.Author.ID)
		_ = s.ChannelMessageDelete(m.ChannelID, m.ID)
		return
	}
	if a.Cfg.DiscordWebhookURL != "" {
		flag := discordwebhook.MessageFlagSuppressNotifications
		username := fmt.Sprintf("%s#%s", m.Author.Username, m.Author.Discriminator)
		content := m.Content
		msg := discordwebhook.Message{
			Content:  &content,
			Username: &username,
			Flags:    &flag,
		}
		if err := discordwebhook.SendMessage(a.Cfg.DiscordWebhookURL, msg); err != nil {
			log.Printf("webhook send failed: %v", err)
		}
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
	log.Printf("Removed whitelist for %s (%s)", entry.Username, ev.User.ID)
}

func (a *App) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		name := i.ApplicationCommandData().Name
		switch name {
		case "list":
			ctx := context.Background()
			out, err := a.Bridge.SendCommand(ctx, "list")
			if err != nil {
				out = "Minecraft not connected"
			}
			a.reply(i, out, true)
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

// --- whitelist (simplified modal) ---

func (a *App) openWhitelistModal(i *discordgo.InteractionCreate) {
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "whitelist_modal",
			Title:    "Whitelist Application",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "mc_username", Label: "Minecraft Username", Style: discordgo.TextInputShort, Required: true},
				}},
			},
		},
	})
}

func (a *App) handleWhitelistSubmit(i *discordgo.InteractionCreate) {
	username := modalValue(i, "mc_username")
	if username == "" {
		a.reply(i, "Missing username", true)
		return
	}
	uuid, err := a.NameMC.UsernameToUUID(username)
	if err != nil {
		a.reply(i, "Could not resolve username", true)
		return
	}
	ctx := context.Background()
	_ = a.WLStore.Add(ctx, i.Member.User.ID, username)
	if a.Bridge.IsConnected() {
		_, _ = a.Bridge.SendCommand(ctx, "whitelist add "+username)
	}
	a.reply(i, fmt.Sprintf("Whitelisted %s (UUID %s). Await staff review.", username, uuid), true)
}

// --- report (simplified) ---

func (a *App) openReportModal(i *discordgo.InteractionCreate) {
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseModal,
		Data: &discordgo.InteractionResponseData{
			CustomID: "report_modal",
			Title:    "Report Issue",
			Components: []discordgo.MessageComponent{
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_type", Label: "Type (player/bug/other)", Style: discordgo.TextInputShort, Required: true},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "reported_username", Label: "Player (if player type)", Style: discordgo.TextInputShort},
				}},
				discordgo.ActionsRow{Components: []discordgo.MessageComponent{
					&discordgo.TextInput{CustomID: "report_reason", Label: "Details", Style: discordgo.TextInputParagraph, Required: true},
				}},
			},
		},
	})
}

func (a *App) handleReportSubmit(i *discordgo.InteractionCreate) {
	t := modalValue(i, "report_type")
	player := modalValue(i, "reported_username")
	reason := modalValue(i, "report_reason")
	embed := &discordgo.MessageEmbed{
		Title:       "New Report",
		Description: "Incoming report",
		Color:       0xF44336,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Reporter", Value: "<@" + i.Member.User.ID + ">", Inline: true},
			{Name: "Type", Value: t, Inline: true},
		},
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	if player != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Player", Value: "`" + player + "`", Inline: true})
	}
	if reason != "" {
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{Name: "Details", Value: reason})
	}
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{Components: []discordgo.MessageComponent{
			&discordgo.Button{CustomID: "report_resolve_" + player + "|" + i.Member.User.ID, Label: "Resolve", Style: discordgo.SuccessButton},
			&discordgo.Button{CustomID: "report_dismiss_" + player + "|" + i.Member.User.ID, Label: "Dismiss", Style: discordgo.DangerButton},
		}},
	}
	_, _ = a.Session.ChannelMessageSendComplex(a.Cfg.ReportChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: components,
	})
	a.reply(i, "Report submitted", true)
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
					&discordgo.TextInput{CustomID: "moderator_note", Label: "Moderator Note", Style: discordgo.TextInputParagraph, Required: true},
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
	orig := parts[2] // original component id
	note := modalValue(i, "moderator_note")
	msg := i.Message
	if msg == nil {
		a.reply(i, "Original message missing", true)
		return
	}
	if len(msg.Embeds) == 0 {
		a.reply(i, "Embed missing", true)
		return
	}
	cp := *msg.Embeds[0]
	cp.Color = 0x22C55E
	label := "Resolved"
	if action == "dismiss" {
		cp.Color = 0xEF4444
		label = "Dismissed"
	}
	line := fmt.Sprintf("📝 %s by <@%s>. Note: %s", label, i.Member.User.ID, note)
	if cp.Description == "" {
		cp.Description = line
	} else {
		cp.Description += "\n\n" + line
	}
	_, _ = a.Session.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    i.ChannelID,
		ID:         i.Message.ID,
		Embeds:     &[]*discordgo.MessageEmbed{&cp},
		Components: &[]discordgo.MessageComponent{},
	})
	a.reply(i, "Updated.", true)
}

// helpers
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
