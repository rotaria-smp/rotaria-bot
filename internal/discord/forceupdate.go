package discord

import (
	"context"
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

func newForceUpdateCommand(perm int64) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:                     "forceupdateusername",
		Description:              "Transfer / update Minecraft username mapping",
		DefaultMemberPermissions: &perm,
		Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		Options: []*discordgo.ApplicationCommandOption{
			{Type: discordgo.ApplicationCommandOptionUser, Name: "discord_user", Description: "Discord user", Required: true},
			{Type: discordgo.ApplicationCommandOptionString, Name: "minecraft_name", Description: "Minecraft username", Required: true},
		},
	}
}

func (a *App) handleForceUpdate(i *discordgo.InteractionCreate) {
	ctx := context.Background()
	if !a.Bridge.IsConnected() {
		a.reply(i, "Minecraft not connected.", true)
		return
	}
	selectedUser := i.ApplicationCommandData().Options[0].UserValue(a.Session)
	newName := i.ApplicationCommandData().Options[1].StringValue()

	uuid, err := a.NameMC.UsernameToUUID(newName)
	if err != nil {
		a.reply(i, fmt.Sprintf("Resolve failed for `%s`: %v", newName, err), true)
		return
	}
	entryByUUID, err := a.WLStore.GetByUUID(ctx, uuid)
	if err != nil {
		a.reply(i, fmt.Sprintf("DB lookup failed: %v", err), true)
		return
	}
	if entryByUUID == nil {
		a.reply(i, fmt.Sprintf("`%s` (UUID %s) not whitelisted; cannot transfer.", newName, uuid), true)
		return
	}
	if entryByUUID.DiscordID == selectedUser.ID {
		if entryByUUID.Username == newName {
			a.reply(i, fmt.Sprintf("<@%s> already linked to `%s`.", selectedUser.ID, newName), true)
			return
		}
		// Username change for same discord
		if err := a.WLStore.UpdateUser(ctx, selectedUser.ID, uuid, newName); err != nil {
			a.reply(i, fmt.Sprintf("Update failed: %v", err), true)
			return
		}
		_ = a.Session.GuildMemberNickname(i.GuildID, selectedUser.ID, newName)
		a.reply(i, fmt.Sprintf("Updated username to `%s`.", newName), true)
		return
	}

	existingDiscordEntry, err := a.WLStore.GetByDiscord(ctx, selectedUser.ID)
	if err != nil {
		a.reply(i, fmt.Sprintf("Discord lookup failed: %v", err), true)
		return
	}
	if existingDiscordEntry != nil && existingDiscordEntry.MinecraftUUID != entryByUUID.MinecraftUUID {
		a.reply(i, fmt.Sprintf("<@%s> already mapped to `%s`.", selectedUser.ID, existingDiscordEntry.Username), true)
		return
	}

	if err := a.WLStore.TransferDiscord(ctx, uuid, selectedUser.ID); err != nil {
		a.reply(i, fmt.Sprintf("Transfer failed: %v", err), true)
		return
	}
	if entryByUUID.Username != newName {
		_ = a.WLStore.UpdateUser(ctx, selectedUser.ID, uuid, newName)
	}
	_ = a.Session.GuildMemberNickname(i.GuildID, selectedUser.ID, newName)

	logging.L().Info("forceupdateusername transfer",
		"old_discord_id", entryByUUID.DiscordID,
		"new_discord_id", selectedUser.ID,
		"uuid", uuid,
		"minecraft_name", newName,
	)

	a.reply(i, fmt.Sprintf("Transferred `%s` to <@%s>.", newName, selectedUser.ID), true)
}
