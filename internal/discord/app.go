package discord

import (
	"time"

	"github.com/bwmarrin/discordgo"
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

	var lookupPerm int64 = discordgo.PermissionBanMembers
	var adminPerm int64 = discordgo.PermissionAdministrator

	cmds := []*discordgo.ApplicationCommand{
		{Name: "list", Description: "List online players"},
		{Name: "whitelist", Description: "Begin whitelist application"},
		{Name: "report", Description: "Report an issue"},
		newLookupCommand(lookupPerm),
		newForceUpdateCommand(adminPerm),
	}

	for _, c := range cmds {
		if _, err := a.Session.ApplicationCommandCreate(a.Session.State.User.ID, "", c); err != nil {
			logging.L().Error("create command failed", "command", c.Name, "err", err)
			return err
		}
	}
	return nil
}
