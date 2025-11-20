package main

import (
	"context"
	"os/signal"
	"syscall"

	"github.com/rotaria-smp/rotaria-bot/internal/discord"
	"github.com/rotaria-smp/rotaria-bot/internal/discord/blacklist"
	"github.com/rotaria-smp/rotaria-bot/internal/mcbridge"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/config"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
	"github.com/rotaria-smp/rotaria-bot/internal/websocket"
	"github.com/rotaria-smp/rotaria-bot/internal/whitelist"
)

func main() {
	logging.BootstrapFromEnv()
	logging.L().Info("starting bot")

	cfg := config.Load()
	if cfg.DiscordToken == "" {
		logging.L().Error("DISCORD_TOKEN not set")
		return
	}

	bot, err := discord.New(cfg.DiscordToken)
	if err != nil {
		logging.L().Error("discord init failed", "err", err)
		return
	}
	if err := bot.Start(); err != nil {
		logging.L().Error("discord open failed", "err", err)
		return
	}
	sess := bot.Session()

	bl, err := blacklist.Load(cfg.BlacklistPath)
	if err != nil {
		logging.L().Warn("blacklist load failed", "err", err)
	} else {
		logging.L().Info("blacklist loaded", "count", len(bl.Words()))
	}

	wlStore, err := whitelist.Open(cfg.DBPath)
	if err != nil {
		logging.L().Error("whitelist store open failed", "err", err, "path", cfg.DBPath)
		return
	}

	bridge := mcbridge.New(nil)
	app := discord.NewApp(sess, cfg, bridge, wlStore, bl)
	if err := app.Register(); err != nil {
		logging.L().Error("command register failed", "err", err)
		return
	}

	bridge.SetHandler(func(topic, body string) {
		app.HandleMCEvent(topic, body)
	})

	hub := websocket.NewHub()
	wsServer := websocket.NewServer(cfg.WSAddr, hub, bridge)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := wsServer.Start(); err != nil {
			logging.L().Error("websocket server error", "err", err)
		}
	}()

	logging.L().Info("bot running", "addr", cfg.WSAddr)
	<-ctx.Done()

	_ = sess.Close()
	logging.L().Info("shutdown complete")
}
