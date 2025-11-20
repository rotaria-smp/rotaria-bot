package config

import "os"

type Config struct {
	DiscordToken      string
	WSAddr            string
	DBPath            string
	DiscordWebhookURL string
	BlacklistPath     string
	GuildID           string
	MemberRoleID      string
}

func Load() Config {
	return Config{
		DiscordToken:      os.Getenv("DISCORD_TOKEN"),
		WSAddr:            envDefault("WS_ADDR", ":8080"),
		DBPath:            envDefault("DB_PATH", "rotaria.db"),
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		BlacklistPath:     envDefault("BLACKLIST_PATH", "Minecraft-connector/discordbot/blacklist.txt"),
		GuildID:           os.Getenv("GUILD_ID"),
		MemberRoleID:      os.Getenv("MEMBER_ROLE_ID"),
	}
}

func envDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
