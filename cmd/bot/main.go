package main

import (
	"context"
	"log"
	"os/signal"
	"syscall"

	"github.com/rotaria-smp/rotaria-bot/internal/discord"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/config"
	ws "github.com/rotaria-smp/rotaria-bot/internal/websocket"
)

func main() {
	cfg := config.Load()
	if cfg.DiscordToken == "" {
		log.Fatal("DISCORD_TOKEN not set")
	}

	bot, err := discord.New(cfg.DiscordToken)
	if err != nil {
		log.Fatalf("discord init: %v", err)
	}

	hub := ws.NewHub()
	wsServer := ws.NewServer(cfg.WSAddr, hub)

	if err := bot.Start(); err != nil {
		log.Fatalf("discord start: %v", err)
	}
	defer bot.Stop()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := wsServer.Start(); err != nil {
			log.Fatalf("websocket server error: %v", err)
		}
	}()

	log.Println("Bot running. Press Ctrl+C to exit.")
	<-ctx.Done()
	log.Println("Shutdown.")
}
