package config

import (
	"os"
)

type Config struct {
	DiscordToken string
	WSAddr       string
}

func Load() Config {
	return Config{
		DiscordToken: os.Getenv("DISCORD_TOKEN"),
		WSAddr:       envDefault("WS_ADDR", ":8080"),
	}
}

func envDefault(key, def string) string {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	return v
}
