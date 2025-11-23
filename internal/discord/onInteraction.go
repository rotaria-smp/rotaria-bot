package discord

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func (a *App) onInteraction(s *discordgo.Session, i *discordgo.InteractionCreate) {
	switch i.Type {
	case discordgo.InteractionApplicationCommand:
		switch i.ApplicationCommandData().Name {
		case "list":
			a.handleListCommand(s, i)
		case "whitelist":
			a.openWhitelistModal(i)
		case "report":
			a.openReportModal(i)
		case "lookup":
			a.handleLookup(i)
		case "forceupdateusername":
			a.handleForceUpdate(i)
		}
	case discordgo.InteractionModalSubmit:
		cid := i.ModalSubmitData().CustomID
		switch {
		case cid == "whitelist_modal":
			a.handleWhitelistSubmit(i)
		case cid == "report_modal":
			a.handleReportSubmit(i)
		case strings.HasPrefix(cid, "report_action_modal|"):
			a.handleReportActionModal(i)
		}
	case discordgo.InteractionMessageComponent:
		c := i.MessageComponentData().CustomID
		switch {
		case c == "request_whitelist":
			a.openWhitelistModal(i)
		case strings.HasPrefix(c, "report_resolve_"), strings.HasPrefix(c, "report_dismiss_"):
			a.openReportActionModal(i)
		case strings.HasPrefix(c, "approve_"), strings.HasPrefix(c, "reject_"):
			a.handleWhitelistDecision(i)
		}
	}
}

func (a *App) handleListCommand(s *discordgo.Session, i *discordgo.InteractionCreate) {
	if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
	}); err != nil {
		logging.L().Warn("list defer failed", "error", err)
		return
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				stack := make([]byte, 8192)
				n := runtime.Stack(stack, false)
				logging.L().Error("list command panic", "recover", r, "stack", string(stack[:n]))
				out := "Internal error during list command, please try again later."
				if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &out}); err != nil {
					logging.L().Error("list panic response edit failed", "error", err)
				}
			}
		}()

		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()

		out, err := a.Bridge.SendCommand(ctx, "list")
		if err != nil {
			out = "Error: " + err.Error()
		}
		if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &out}); err != nil {
			logging.L().Error("list response edit failed", "error", err)
		}
	}()
}

func (a *App) reply(i *discordgo.InteractionCreate, msg string, eph bool) {
	flags := discordgo.MessageFlags(0)
	if eph {
		flags = discordgo.MessageFlagsEphemeral
	}
	_ = a.Session.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{Content: msg, Flags: flags},
	})
}
