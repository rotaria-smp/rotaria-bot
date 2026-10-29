package discord

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/rotaria-smp/rotaria-bot/internal/shared/logging"
)

var modrinthLinkRe = regexp.MustCompile(`modrinth\.com/[a-z]+/([^/?#\s]+)/version/([^/?#\s]+)`)

var modrinthClient = &http.Client{Timeout: 8 * time.Second}

func newNewCommand(perm int64) *discordgo.ApplicationCommand {
	return &discordgo.ApplicationCommand{
		Name:                     "new",
		Description:              "Announcements",
		DefaultMemberPermissions: &perm,
		Contexts:                 &[]discordgo.InteractionContextType{discordgo.InteractionContextGuild},
		Options: []*discordgo.ApplicationCommandOption{{
			Type:        discordgo.ApplicationCommandOptionSubCommand,
			Name:        "version",
			Description: "Announce a new Modrinth version",
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "link", Description: "Modrinth version link", Required: true},
			},
		}},
	}
}

func modrinthGet(path string, out any) error {
	req, _ := http.NewRequest("GET", "https://api.modrinth.com/v2/"+path, nil)
	req.Header.Set("User-Agent", "rotaria-smp/rotaria-bot")
	resp, err := modrinthClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("modrinth returned %s", resp.Status)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (a *App) handleNewVersion(i *discordgo.InteractionCreate) {
	log := logging.L().With("component", "discord", "module", "newversion", "func", "handleNewVersion")
	if a.Cfg.UpdatesChannelID == "" {
		a.reply(i, "UPDATES_CHANNEL_ID is not configured.", true)
		return
	}
	link := i.ApplicationCommandData().Options[0].Options[0].StringValue()
	m := modrinthLinkRe.FindStringSubmatch(link)
	if m == nil {
		a.reply(i, "Expected a link like `https://modrinth.com/modpack/<slug>/version/<version>`.", true)
		return
	}
	slug, ver := url.PathEscape(m[1]), url.PathEscape(m[2])

	var project struct {
		Title   string `json:"title"`
		IconURL string `json:"icon_url"`
	}
	var version struct {
		Name          string `json:"name"`
		VersionNumber string `json:"version_number"`
		Changelog     string `json:"changelog"`
		Files         []struct {
			URL     string `json:"url"`
			Primary bool   `json:"primary"`
		} `json:"files"`
	}
	if err := modrinthGet("project/"+slug, &project); err != nil {
		a.reply(i, "Modrinth project lookup failed: "+err.Error(), true)
		return
	}
	if err := modrinthGet("project/"+slug+"/version/"+ver, &version); err != nil {
		a.reply(i, "Modrinth version lookup failed: "+err.Error(), true)
		return
	}

	download := link
	for n, f := range version.Files {
		if f.Primary || n == 0 {
			download = f.URL
		}
		if f.Primary {
			break
		}
	}
	changelog := strings.TrimSpace(version.Changelog)
	if changelog == "" {
		changelog = "No changelog provided."
	}
	if r := []rune(changelog); len(r) > 3500 {
		changelog = string(r[:3500]) + "..."
	}

	embed := &discordgo.MessageEmbed{
		Title:       fmt.Sprintf("%s %s", project.Title, version.VersionNumber),
		URL:         link,
		Description: changelog,
		Color:       0x1bd96a,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Version", Value: version.Name, Inline: true},
			{Name: "Download", Value: fmt.Sprintf("[Download](%s)", download), Inline: true},
		},
		Footer: &discordgo.MessageEmbedFooter{Text: "Modrinth"},
	}
	if project.IconURL != "" {
		embed.Thumbnail = &discordgo.MessageEmbedThumbnail{URL: project.IconURL}
	}

	_, err := a.Session.ChannelMessageSendComplex(a.Cfg.UpdatesChannelID, &discordgo.MessageSend{
		Content:         fmt.Sprintf("<@&%s>", a.Cfg.MemberRoleID),
		Embeds:          []*discordgo.MessageEmbed{embed},
		AllowedMentions: &discordgo.MessageAllowedMentions{Roles: []string{a.Cfg.MemberRoleID}},
	})
	if err != nil {
		log.Error("send failed", "err", err)
		a.reply(i, "Failed to post: "+err.Error(), true)
		return
	}
	a.reply(i, "Posted.", true)
}
