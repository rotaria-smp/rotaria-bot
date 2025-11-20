package namemc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
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
		uuidAPI: "https://sessionserver.mojang.com/session/minecraft/profile/",
	}
}

func (c *Client) UsernameToUUID(username string) (string, error) {
	if username == "" {
		return "", errors.New("username required")
	}
	url := fmt.Sprintf("%s%s?at=%d", c.apiURL, username, time.Now().Unix())
	var out struct {
		ID string `json:"id"`
	}
	if err := c.getJSON(url, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("uuid not found for %q", username)
	}
	return out.ID, nil
}

func (c *Client) getJSON(url string, out any) error {
	r, err := c.http.Get(url)
	if err != nil {
		return err
	}
	defer r.Body.Close()
	if r.StatusCode >= 400 {
		b, _ := io.ReadAll(r.Body)
		return fmt.Errorf("GET %s: %s: %s", url, r.Status, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(r.Body).Decode(out)
}
