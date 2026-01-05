package namemc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

type Client struct {
	http    *http.Client
	apiURL  string
	uuidAPI string
}

func New() *Client {
	return &Client{
		http:    &http.Client{Timeout: 15 * time.Second},
		apiURL:  "https://api.mojang.com/users/profiles/minecraft/",
		uuidAPI: "https://api.minecraftservices.com/minecraft/profile/lookup/",
	}
}

func (c *Client) UsernameToUUID(username string) (string, error) {
	if username == "" {
		return "", errors.New("username required")
	}
	url := fmt.Sprintf("%s%s?at=%d", c.apiURL, username, time.Now().Unix())
	log := logging.L().With(
		"component", "discord",
		"module", "namemc",
		"func", "UsernameToUUID",
		"username", username,
	)
	log.Info("start", "url", url)

	var out struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(url, &out); err != nil {
		log.Error("getJSON failed", "error", err, "url", url)
		return "", err
	}

	log.Info("response", "id", out.ID)

	if out.ID == "" {
		return "", fmt.Errorf("uuid not found for %q", username)
	}
	return out.ID, nil
}

func (c *Client) UUIDToUsername(uuid string) (string, error) {
	if uuid == "" {
		return "", errors.New("UUID required")
	}

	url := fmt.Sprintf("%s%s?at=%d", c.uuidAPI, uuid, time.Now().Unix())

	log := logging.L().With(
		"component", "discord",
		"module", "namemc",
		"func", "UUIDToUsername",
		"uuid", uuid,
	)
	log.Info("start", "url", url)

	var out struct {
		Name string `json:"name"`
	}

	if err := c.getJSON(url, &out); err != nil {
		log.Error("getJSON failed", "error", err, "url", url)
		return "", err
	}

	if out.Name == "" {
		return "", fmt.Errorf("username not found for UUID %q", uuid)
	}
	log.Info("response", "name", out.Name)
	return out.Name, nil
}

func (c *Client) getJSON(url string, out any) error {
	log := logging.L().With(
		"component", "discord",
		"module", "namemc",
		"func", "getJSON",
		"url", url,
	)
	log.Debug("GET")

	r, err := c.http.Get(url)
	if err != nil {
		log.Error("GET failed", "error", err)
		return err
	}
	defer r.Body.Close()

	log.Debug("GET response", "status", r.StatusCode)

	if r.StatusCode >= 400 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("GET %s: %s: %s", url, r.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(r.Body).Decode(out)
}
