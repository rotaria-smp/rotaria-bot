package discord

import (
	"strings"

	"github.com/bwmarrin/discordgo"
)

func ternary[T any](cond bool, a T, b T) T {
	if cond {
		return a
	}
	return b
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

func actorID(i *discordgo.InteractionCreate) string {
	if i.Member != nil && i.Member.User != nil {
		return i.Member.User.ID
	}
	if i.User != nil {
		return i.User.ID
	}
	return ""
}
