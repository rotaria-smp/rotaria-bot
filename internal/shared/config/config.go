package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

type Config struct {
	DiscordToken                       string
	WSAddr                             string
	DBPath                             string
	DiscordWebhookURL                  string
	BlacklistPath                      string
	GuildID                            string
	MemberRoleID                       string
	ReportChannelID                    string
	WhitelistRequestsChannelID         string
	MinecraftDiscordMessengerChannelID string
	ServerStatusChannelID              string
}

func Load() Config {
	loadDotEnv()
	return Config{
		DiscordToken:                       os.Getenv("DISCORD_TOKEN"),
		WSAddr:                             envDefault("WS_ADDR", ":8080"),
		DBPath:                             envDefault("DB_PATH", "./database.db"),
		DiscordWebhookURL:                  os.Getenv("DISCORD_WEBHOOK_URL"),
		BlacklistPath:                      envDefault("BLACKLIST_PATH", "./blacklist.txt"),
		GuildID:                            os.Getenv("GUILD_ID"),
		MemberRoleID:                       os.Getenv("MEMBER_ROLE_ID"),
		ReportChannelID:                    os.Getenv("REPORT_CHANNEL_ID"),
		WhitelistRequestsChannelID:         os.Getenv("WHITELIST_REQUESTS_CHANNEL_ID"),
		MinecraftDiscordMessengerChannelID: os.Getenv("MinecraftDiscordMessengerChannelID"),
		ServerStatusChannelID:              os.Getenv("ServerStatusChannelID"),
	}
}

func envDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}

func loadDotEnv() {
	path := os.Getenv("ENV_FILE")
	if path != "" {
		if err := godotenv.Load(path); err != nil {
			log.Printf("env: could not load %s: %v", path, err)
		}
		return
	}
	// Try default .env silently
	_ = godotenv.Load()
}
