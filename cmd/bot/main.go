package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/discord"
	"github.com/rotaria-smp/rotaria-bot/internal/discord/blacklist"
	"github.com/rotaria-smp/rotaria-bot/internal/mcbridge"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/config"
	"github.com/rotaria-smp/rotaria-bot/internal/websocket"
	"github.com/rotaria-smp/rotaria-bot/internal/whitelist"
)

func main() {
	cfg := config.Load()
	if cfg.DiscordToken == "" {
		log.Fatal("DISCORD_TOKEN not set")
	}

	// Discord session
	sess, err := discordgo.New("Bot " + cfg.DiscordToken)
	if err != nil {
		log.Fatalf("discord init: %v", err)
	}
	sess.Identify.Intents = discordgo.IntentsGuilds |
		discordgo.IntentsGuildMessages |
		discordgo.IntentsGuildMembers

	if err := sess.Open(); err != nil {
		log.Fatalf("discord open: %v", err)
	}

	bl, err := blacklist.Load(cfg.BlacklistPath)
	if err != nil {
		log.Printf("blacklist load failed: %v", err)
	}

	wlStore, err := whitelist.Open(cfg.DBPath)
	if err != nil {
		log.Fatalf("whitelist store: %v", err)
	}

	bridge := mcbridge.New(func(topic, body string) {
		// handle inbound Minecraft events if needed
	})

	app := discord.NewApp(sess, cfg, bridge, wlStore, bl)
	if err := app.Register(); err != nil {
		log.Fatalf("register: %v", err)
	}

	hub := websocket.NewHub()
	wsServer := websocket.NewServer(cfg.WSAddr, hub, bridge)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := wsServer.Start(); err != nil {
			log.Fatalf("websocket server error: %v", err)
		}
	}()

	log.Println("Bot running. Ctrl+C to exit.")
	<-ctx.Done()

	_ = sess.Close()
	log.Println("Shutdown.")
}
