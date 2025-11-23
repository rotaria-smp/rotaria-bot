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
	Session          *discordgo.Session
	Cfg              config.Config
	Bridge           *mcbridge.Bridge
	WLStore          *whitelist.Store
	Blacklist        *blacklist.List
	NameMC           *namemc.Client
	lastStatusUpdate time.Time
}

var chatLineRe = regexp.MustCompile(`^<([^>]+)>[ ]?(.*)$`)
var atEveryone = regexp.MustCompile(`@everyone`)
var joinLineRe = regexp.MustCompile(`^\*\*([A-Za-z0-9_]+)\*\* joined the server\.$`)

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

	// IDK what permission here is wanted we'll use ban permission for now
	var lookupCommandPermissions int64 = discordgo.PermissionBanMembers
	var administratorPermissions int64 = discordgo.PermissionAdministrator

	commands := []*discordgo.ApplicationCommand{
		{Name: "list", Description: "List online players"},
		{Name: "whitelist", Description: "Begin whitelist application"},
		{Name: "report", Description: "Report an issue"},
		{
			Name:                     "lookup",
			Description:              "Lookup Minecraft username (admin only)",
			DefaultMemberPermissions: &lookupCommandPermissions,
			Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "discord_user",
					Description: "Discord user ID to lookup",
					Required:    false,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "minecraft_name",
					Description: "Minecraft username to lookup",
					Required:    false,
				},
			},
		},
		{
			Name:        "forceupdateusername",
			Description: "Forcefully update minecraft username in database and rewhitelists them on the server, this will also transfer minecraft ownership to selected user if a discord account is already connected to it",
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionUser,
					Name:        "discord_user",
					Description: "Discord user ID to alter",
					Required:    true,
				},
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "minecraft_name",
					Description: "The players new minecraft namn",
					Required:    true,
				},
			},
			DefaultMemberPermissions: &administratorPermissions,
			Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		},
	}
	for _, c := range commands {
		if _, err := a.Session.ApplicationCommandCreate(a.Session.State.User.ID, "", c); err != nil {
			logging.L().Error("failed to create application command", "command", c.Name, "err", err)
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
		case "lookup":
			// Deferred ephemeral response to allow network calls (UUID/name resolution)
			if err := s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
				Type: discordgo.InteractionResponseDeferredChannelMessageWithSource,
				Data: &discordgo.InteractionResponseData{Flags: discordgo.MessageFlagsEphemeral},
			}); err != nil {
				logging.L().Warn("lookup: defer respond failed", "error", err)
				return
			}
			go func() {
				ctx := context.Background()
				// Build a map of provided options (only provided options appear in the slice)
				optMap := make(map[string]*discordgo.ApplicationCommandInteractionDataOption)
				for _, o := range i.ApplicationCommandData().Options {
					optMap[o.Name] = o
				}

				var response string
				if o, ok := optMap["discord_user"]; ok {
					selectedUser := o.UserValue(s)
					entry, err := a.WLStore.GetByDiscord(ctx, selectedUser.ID)
					if err != nil {
						response = fmt.Sprintf("Could not lookup <@%s>: %v", selectedUser.ID, err)
					} else if entry == nil {
						response = fmt.Sprintf("<@%s> is not whitelisted", selectedUser.ID)
					} else {
						response = fmt.Sprintf("<@%s> has the Minecraft name `%s`", selectedUser.ID, entry.Username)
					}
				} else if o, ok := optMap["minecraft_name"]; ok {
					name := strings.TrimSpace(o.StringValue())
					if name == "" {
						response = "Minecraft name cannot be empty"
					} else {
						uuid, err := a.NameMC.UsernameToUUID(name)
						if err != nil {
							// Fallback: direct DB lookup by username if external resolution fails
							entry, dbErr := a.WLStore.GetByUsername(ctx, name)
							if dbErr != nil {
								response = fmt.Sprintf("Could not resolve `%s` and DB lookup failed: %v", name, dbErr)
							} else if entry == nil {
								response = fmt.Sprintf("Could not resolve `%s` (UUID service error) and name not found in whitelist DB", name)
							} else {
								response = fmt.Sprintf("`%s` appears whitelisted (Discord <@%s>) despite UUID resolution error)", entry.Username, entry.DiscordID)
							}
						} else {
							entry, err := a.WLStore.GetByUUID(ctx, uuid)
							if err != nil {
								response = fmt.Sprintf("Lookup failed for `%s`: %v", name, err)
							} else if entry == nil {
								response = fmt.Sprintf("`%s` (UUID %s) is not in whitelist DB", name, uuid)
							} else {
								response = fmt.Sprintf("`%s` is whitelisted (Discord <@%s>)", entry.Username, entry.DiscordID)
							}
						}
					}
				} else {
					response = "Provide at least one option: discord_user / minecraft_name"
				}

				if _, err := s.InteractionResponseEdit(i.Interaction, &discordgo.WebhookEdit{Content: &response}); err != nil {
					logging.L().Warn("lookup: InteractionResponseEdit failed", "error", err)
				}
			}()
			return
		case "forceupdateusername":
			ctx := context.Background()
			if !a.Bridge.IsConnected() {
				a.reply(i, "Minecraft server is not connected; cannot transfer right now.", true)
				return
			}
			selectedUser := i.ApplicationCommandData().Options[0].UserValue(s)
			newMinecraftName := i.ApplicationCommandData().Options[1].StringValue()

			uuid, err := a.NameMC.UsernameToUUID(newMinecraftName)
			if err != nil {
				logging.L().Error("forceupdateusername: resolve username->UUID failed", "username", newMinecraftName, "error", err)
				a.reply(i, fmt.Sprintf("Could not resolve `%s`: %v", newMinecraftName, err), true)
				return
			}

			entryByUUID, err := a.WLStore.GetByUUID(ctx, uuid)
			if err != nil {
				logging.L().Error("forceupdateusername: GetByUUID failed", "uuid", uuid, "error", err)
				a.reply(i, fmt.Sprintf("Lookup failed for `%s`: %v", newMinecraftName, err), true)
				return
			}
			if entryByUUID == nil {
				a.reply(i, fmt.Sprintf("`%s` (UUID %s) is not currently whitelisted; cannot transfer.", newMinecraftName, uuid), true)
				return
			}

			// Already linked to this discord user; allow force name update if changed and re-whitelist
			if entryByUUID.DiscordID == selectedUser.ID {
				if entryByUUID.Username == newMinecraftName {
					a.reply(i, fmt.Sprintf("<@%s> is already linked to `%s` (no change).", selectedUser.ID, newMinecraftName), true)
					return
				}
				oldName := entryByUUID.Username
				// Attempt to whitelist new name then unwhitelist old to avoid downtime
				if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("whitelist add %s", newMinecraftName)); err != nil {
					logging.L().Error("forceupdateusername: whitelist add failed", "new_name", newMinecraftName, "error", err)
					a.reply(i, fmt.Sprintf("Failed to whitelist new name `%s`: %v", newMinecraftName, err), true)
					return
				}
				if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("unwhitelist %s", oldName)); err != nil {
					logging.L().Warn("forceupdateusername: unwhitelist old name failed", "old_name", oldName, "error", err)
				}
				// Update stored username and nickname
				if err := a.WLStore.UpdateUser(ctx, selectedUser.ID, uuid, newMinecraftName); err != nil {
					logging.L().Error("forceupdateusername: UpdateUser failed for existing link", "discord_id", selectedUser.ID, "uuid", uuid, "error", err)
					a.reply(i, fmt.Sprintf("Failed to update username to `%s`: %v (Minecraft whitelist may now reference new name)", newMinecraftName, err), true)
					return
				}
				if err := a.Session.GuildMemberNickname(i.GuildID, selectedUser.ID, newMinecraftName); err != nil {
					logging.L().Error("forceupdateusername: nickname update failed (existing link)", "discord_id", selectedUser.ID, "error", err)
				}
				logging.L().Info("forceupdateusername: updated existing linked username",
					"discord_id", selectedUser.ID,
					"old_username", oldName,
					"new_username", newMinecraftName,
					"uuid", uuid,
				)
				a.reply(i, fmt.Sprintf("Updated Minecraft username for <@%s> from `%s` to `%s` (re-whitelisted).", selectedUser.ID, oldName, newMinecraftName), true)
				return
			}

			// Check if selected user already has another whitelist entry
			existingDiscordEntry, err := a.WLStore.GetByDiscord(ctx, selectedUser.ID)
			if err != nil {
				logging.L().Error("forceupdateusername: GetByDiscord failed", "discord_id", selectedUser.ID, "error", err)
				a.reply(i, fmt.Sprintf("Could not verify existing whitelist for <@%s>: %v", selectedUser.ID, err), true)
				return
			}
			if existingDiscordEntry != nil && existingDiscordEntry.MinecraftUUID != entryByUUID.MinecraftUUID {
				a.reply(i, fmt.Sprintf("<@%s> already has a different whitelisted Minecraft account (`%s`). Transfer aborted.", selectedUser.ID, existingDiscordEntry.Username), true)
				return
			}

			// Transfer discord_id to selectedUser.ID (no whitelist change if name same; if name changed re-whitelist)
			oldName := entryByUUID.Username
			if err := a.WLStore.TransferDiscord(ctx, uuid, selectedUser.ID); err != nil {
				logging.L().Error("forceupdateusername: TransferDiscord failed", "uuid", uuid, "new_discord_id", selectedUser.ID, "error", err)
				a.reply(i, fmt.Sprintf("Transfer failed for `%s`: %v", newMinecraftName, err), true)
				return
			}

			// If the provided new name differs, re-whitelist update
			if oldName != newMinecraftName {
				if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("whitelist add %s", newMinecraftName)); err != nil {
					logging.L().Error("forceupdateusername: whitelist add failed (transfer)", "new_name", newMinecraftName, "error", err)
					a.reply(i, fmt.Sprintf("Transfer done, but failed to whitelist new name `%s`: %v", newMinecraftName, err), true)
				}
				if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("unwhitelist %s", oldName)); err != nil {
					logging.L().Warn("forceupdateusername: unwhitelist old name failed (transfer)", "old_name", oldName, "error", err)
				}
				if err := a.WLStore.UpdateUser(ctx, selectedUser.ID, uuid, newMinecraftName); err != nil {
					logging.L().Warn("forceupdateusername: UpdateUser name sync failed", "error", err)
				}
			}

			// Update nickname
			if err := a.Session.GuildMemberNickname(i.GuildID, selectedUser.ID, newMinecraftName); err != nil {
				logging.L().Error("forceupdateusername: nickname update failed", "discord_id", selectedUser.ID, "error", err)
			}

			logging.L().Info("forceupdateusername: transferred discord_id",
				"old_discord_id", entryByUUID.DiscordID,
				"new_discord_id", selectedUser,
				"minecraft_name", newMinecraftName,
				"uuid", uuid,
			)

			a.reply(i, fmt.Sprintf("Transferred whitelist entry for `%s` to <@%s>.", newMinecraftName, selectedUser), true)
			return
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
	if !a.Bridge.IsConnected() {
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

		/*
			1. Try to whitelist user on minecraft, exit if failed
			2. Try to add member role, exit if failed
			3. Try to save entry to database, exit if failed
			4. Try to rename guild user to minecraft username, Exit if failed
		*/
		if _, err := a.Bridge.SendCommand(ctx, fmt.Sprintf("whitelist add %s", username)); err != nil {
			logging.L().Error("Failed to send whitelist add command to bridge", "error", err)
			a.reply(i, fmt.Sprintf("Failed to send whitelist command to minecraft server, please try again or try contacting @<@%s>", "322015089529978880"), true)
			return
		}

		if err := a.Session.GuildMemberRoleAdd(a.Cfg.GuildID, requesterID, a.Cfg.MemberRoleID); err != nil {
			logging.L().Error("Failed to assign member role during whitelist decision", "error", err)
			a.reply(i, fmt.Sprintf("Failed to assign member role, please try again or try contacting @<@%s>", "322015089529978880"), true)
			return
		}

		if err := a.WLStore.Add(ctx, requesterID, uuid, username); err != nil {
			logging.L().Error("Failed to add whitelist entry to database", "error", err)
			a.reply(i, fmt.Sprintf("Failed to assign member role, please try again or try contacting @<@%s>", "322015089529978880"), true)
			return
		}

		if err = a.Session.GuildMemberNickname(i.GuildID, requesterID, username); err != nil {
			logging.L().Error("Failed to set guild member nickname during whitelist decision", "error", err)
			a.reply(i, fmt.Sprintf("Failed to set your nickname, please try again or try contacting @<@%s>", "322015089529978880"), true)
		}

		if dm, err := a.Session.UserChannelCreate(requesterID); err == nil {
			_, _ = a.Session.ChannelMessageSend(dm.ID, fmt.Sprintf("✅ You have been whitelisted on Rotaria! Welcome to Rotaria, `%s` 🎉", username))
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

func (a *App) handlePlayerJoinSync(mcName string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	uuid, err := a.NameMC.UsernameToUUID(mcName)
	if err != nil {
		// This will happen for offline/Bedrock/etc – just log and bail out
		logging.L().Warn("handlePlayerJoinSync: UsernameToUUID failed",
			"minecraft_name", mcName,
			"error", err,
		)
		return
	}

	logging.L().Debug("handlePlayerJoinSync: resolved username to UUID",
		"minecraft_name", mcName,
		"uuid", uuid,
	)

	entry, err := a.WLStore.GetByUUID(ctx, uuid)
	if err != nil {
		logging.L().Error("handlePlayerJoinSync: DB lookup failed",
			"minecraft_name", mcName,
			"uuid", uuid,
			"error", err,
		)
		return
	}
	if entry == nil {
		// They might not be whitelisted via Discord (e.g. whitelisted manually on server) this is bad
		logging.L().Warn("handlePlayerJoinSync: no DB entry for UUID",
			"minecraft_name", mcName,
			"uuid", uuid,
		)
		return
	}

	if entry.Username != mcName {
		logging.L().Info("handlePlayerJoinSync: username changed, updating DB",
			"old_username", entry.Username,
			"new_username", mcName,
			"uuid", uuid,
			"discord_id", entry.DiscordID,
		)

		if err := a.WLStore.UpdateUser(ctx, entry.DiscordID, uuid, mcName); err != nil {
			logging.L().Error("handlePlayerJoinSync: failed to update DB username",
				"minecraft_name", mcName,
				"uuid", uuid,
				"discord_id", entry.DiscordID,
				"error", err,
			)
			return
		}
	}

	if err := a.Session.GuildMemberNickname(a.Cfg.GuildID, entry.DiscordID, mcName); err != nil {
		logging.L().Error("handlePlayerJoinSync: failed to update discord nickname",
			"minecraft_name", mcName,
			"uuid", uuid,
			"discord_id", entry.DiscordID,
			"error", err,
		)
		return
	}

	logging.L().Info("handlePlayerJoinSync: updated discord nickname",
		"minecraft_name", mcName,
		"uuid", uuid,
		"discord_id", entry.DiscordID,
	)
}

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

		// Rate limit status updates to once per minute
		now := time.Now()
		if now.Sub(a.lastStatusUpdate) < time.Minute {
			logging.L().Debug("HandleMCEvent: skipping status update due to rate limit")
			return
		}

		if err := a.Session.UpdateGameStatus(0, body); err != nil {
			logging.L().Error("HandleMCEvent: failed to update presence", "error", err)
		} else {
			logging.L().Debug("HandleMCEvent: updated presence", "presence", body)
			a.lastStatusUpdate = now
		}
		return
	}

	// If a user joins the mc server, lets update the discord nick to match the ingame name
	if topic == "join" {
		logging.L().Debug("Player joined", "message", body)

		if m := joinLineRe.FindStringSubmatch(body); m != nil {
			mcName := m[1] // e.g. "limp4n__"
			logging.L().Debug("Parsed join username", "minecraft_name", mcName)

			// sync in background so we don't block event handling
			go a.handlePlayerJoinSync(mcName)
		}

		a.sendWebhook("Rotaria", body, "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp")
		return
	}

	if topic == "join" || topic == "leave" || topic == "lifecycle" {
		a.sendWebhook("Rotaria", body, "https://cdn.discordapp.com/icons/1373389493218050150/24f94fe60c73b4af4956f10dbecb5919.webp")
		return
	}

	if topic == "chat" {
		msg := body
		fullUsername := "server"
		minecraftName := "server"

		if m := chatLineRe.FindStringSubmatch(body); m != nil {
			// m[1] is e.g. "[Owner] Awiant"
			fullUsername = m[1]

			// Take only the last word as the MC name
			nameRe := regexp.MustCompile(`([A-Za-z0-9_]+)$`)
			if n := nameRe.FindStringSubmatch(fullUsername); len(n) > 1 {
				minecraftName = n[1] // "Awiant"
			} else {
				minecraftName = fullUsername
			}

			msg = m[2]
		}

		msg = atEveryone.ReplaceAllString(msg, "\"everyone")

		if a.Blacklist != nil && a.Blacklist.Contains(msg) {
			logging.L().Info("Blocked message from user (blacklist hit)", "message", msg, "user", minecraftName)
			if a.Bridge.IsConnected() {
				ctx := context.Background()
				_, _ = a.Bridge.SendCommand(ctx, fmt.Sprintf("kick %s", minecraftName))
			}
			return
		}

		if strings.TrimSpace(msg) == "" {
			return
		}

		a.sendWebhook(fullUsername, msg, fmt.Sprintf("https://minotar.net/avatar/%s/128.png", minecraftName))
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
		logging.L().Error("sendWebhook: webhook send fail", "error", err, "username", username, "content", content, "avatar", avatar)
	}
}
