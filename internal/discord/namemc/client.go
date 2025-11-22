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

	logging.L().Info("NameMC: UsernameToUUID start", "username", username, "url", url)

	var out struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(url, &out); err != nil {
		logging.L().Error("NameMC: UsernameToUUID getJSON failed", "username", username, "error", err)
		return "", err
	}

	logging.L().Info("NameMC: UsernameToUUID got response", "username", username, "id", out.ID)

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

	var out struct {
		Name string `json:"name"`
	}

	if err := c.getJSON(url, &out); err != nil {
		return "", err
	}

	if out.Name == "" {
		return "", fmt.Errorf("username not found for UUID %q", uuid)
	}

	return out.Name, nil
}

func (c *Client) getJSON(url string, out any) error {
	logging.L().Debug("NameMC: GET", "url", url)

	r, err := c.http.Get(url)
	if err != nil {
		logging.L().Error("NameMC: GET failed", "url", url, "error", err)
		return err
	}
	defer r.Body.Close()

	logging.L().Debug("NameMC: GET response", "url", url, "status", r.StatusCode)

	if r.StatusCode >= 400 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("GET %s: %s: %s", url, r.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(r.Body).Decode(out)
}
