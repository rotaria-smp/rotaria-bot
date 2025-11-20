package discord

import (
	"fmt"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

type Bot struct {
	session *discordgo.Session
}

func New(token string) (*Bot, error) {
	s, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, err
	}
	s.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers
	b := &Bot{session: s}
	s.AddHandler(b.onReady)
	return b, nil
}

func (b *Bot) onReady(_ *discordgo.Session, r *discordgo.Ready) {
	logging.L().Info("Discord connected as", "user", fmt.Sprintf("%s#%s", r.User.Username, r.User.Discriminator))
}

func (b *Bot) Start() error {
	err := b.session.Open()
	if err == nil {
		logging.L().Debug("gateway connected", "sessionID", b.session.State.SessionID)
	}
	return err
}

func (b *Bot) Stop() {
	_ = b.session.Close()
}

func (b *Bot) Session() *discordgo.Session { return b.session }
