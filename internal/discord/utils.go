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
