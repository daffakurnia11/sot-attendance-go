// Package playerreport renders frozen server-presence evidence without browser sessions or web credentials.
package playerreport

import (
	"bytes"
	"context"
	"embed"
	"encoding/base64"
	"fmt"
	"html/template"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
)

type Player struct {
	Name          string `json:"name"`
	Username      string `json:"username"`
	ServerID      string `json:"server_id"`
	ServerStatus  string `json:"server_status,omitempty"`
	DisplayName   string `json:"display_name,omitempty"`
	DiscordStatus string `json:"discord_status,omitempty"`
	ServerName    string `json:"server_name,omitempty"`
	CID           string `json:"cid,omitempty"`
}
type Snapshot struct {
	At      time.Time `json:"at"`
	Players []Player  `json:"players"`
}

func Capture(at time.Time, players []dashboard.Player) Snapshot {
	result := Snapshot{At: at, Players: []Player{}}
	seen := map[string]bool{}
	for _, p := range players {
		if p.MemberID == nil || p.DiscordUserID == "" || (p.Status != "connected" && p.Status != "connecting") || (p.Status == "connected" && (p.ServerID == nil || *p.ServerID == "")) || seen[p.DiscordUserID] {
			continue
		}
		seen[p.DiscordUserID] = true
		name := p.CharacterName
		if name == "" {
			name = p.DisplayName
		}
		slot := ""
		if p.ServerID != nil {
			slot = *p.ServerID
		}
		result.Players = append(result.Players, Player{ServerStatus: p.Status, Name: name, Username: p.Username, ServerID: slot, DisplayName: p.DisplayName, DiscordStatus: discordStatus(p), ServerName: p.CFXName, CID: p.CID})
	}
	sort.Slice(result.Players, func(i, j int) bool { return result.Players[i].Name < result.Players[j].Name })
	return result
}

// ConnectedCount excludes connecting rows. Legacy snapshots contain connected rows only.
func (s Snapshot) ConnectedCount() int {
	count := 0
	for _, p := range s.Players {
		if p.ServerStatus == "connected" || p.ServerStatus == "" {
			count++
		}
	}
	return count
}

func WindowDate(now time.Time, loc *time.Location) (string, bool) {
	now = now.In(loc)
	return now.Format("2006-01-02"), now.Hour() >= 21
}
func Caption(s Snapshot, peak bool, loc *time.Location) string {
	at := s.At.In(loc)
	return fmt.Sprintf("```\nTanggal : %s\nWaktu   : %s WIB\nJumlah  : %d Orang\n```", at.Format("02/01/2006"), at.Format("15:04:05"), s.ConnectedCount())
}

//go:embed report.html assets/*
var assets embed.FS

func discordStatus(p dashboard.Player) string {
	if p.DiscordStatus == "unknown" || p.DiscordStatus == "" {
		return "unknown"
	}
	if p.DiscordStatus == "offline" || p.DiscordStatus == "invisible" {
		return "offline"
	}
	if p.DiscordConnecting {
		return "connecting"
	}
	if p.DiscordPlaying {
		return "connected"
	}
	return "noactivity"
}
func assetURL(name, mime string) template.URL {
	data, err := assets.ReadFile("assets/" + name)
	if err != nil {
		panic(err)
	}
	return template.URL("data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data))
}
func HTML(s Snapshot, loc *time.Location) ([]byte, error) {
	source, err := assets.ReadFile("report.html")
	if err != nil {
		return nil, err
	}
	labels := map[string]string{"connected": "Connected", "connecting": "Connecting", "offline": "Offline / invisible", "noactivity": "No CR activity", "unknown": "Unknown"}
	page, err := template.New("report").Funcs(template.FuncMap{
		"row": func(index int) string { return fmt.Sprintf("%02d", index+1) },
		"discordStatus": func(status string) string {
			if label, ok := labels[status]; ok {
				return label
			}
			return "Unknown"
		},
		"statusClass": func(status string) string {
			if _, ok := labels[status]; ok {
				return status
			}
			return "unknown"
		},
	}).Parse(string(source))
	if err != nil {
		return nil, err
	}
	count := 0
	for _, player := range s.Players {
		if player.DiscordStatus == "connected" {
			count++
		}
	}
	var out bytes.Buffer
	err = page.Execute(&out, struct {
		Time                        string
		Players                     []Player
		DiscordCount                int
		Logo, DisplayFont, BodyFont template.URL
	}{s.At.In(loc).Format("02 January 2006 · 15:04:05"), s.Players, count, assetURL("sot-logo.png", "image/png"), assetURL("Anton-Regular.ttf", "font/ttf"), assetURL("BarlowCondensed-Regular.ttf", "font/ttf")})
	return out.Bytes(), err
}
func Render(ctx context.Context, s Snapshot, loc *time.Location) ([]byte, error) {
	if len(s.Players) > 200 {
		return nil, fmt.Errorf("report exceeds 200-member image limit")
	}
	html, err := HTML(s, loc)
	if err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp("", "sot-player-report-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	source := filepath.Join(dir, "report.html")
	output := filepath.Join(dir, "report.png")
	if err = os.WriteFile(source, html, 0600); err != nil {
		return nil, err
	}
	binary, err := exec.LookPath("chromium-browser")
	if err != nil {
		binary, err = exec.LookPath("chromium")
	}
	if err != nil {
		return nil, fmt.Errorf("Chromium renderer unavailable: %w", err)
	}
	// Only escaped, self-contained HTML is rendered. No authenticated browser profile or remote assets.
	command := exec.CommandContext(ctx, binary, "--headless", "--no-sandbox", "--disable-gpu", "--disable-dev-shm-usage", "--disable-background-networking", "--no-first-run", "--hide-scrollbars", "--user-data-dir="+filepath.Join(dir, "profile"), fmt.Sprintf("--window-size=1200,%d", reportHeight(len(s.Players))), "--screenshot="+output, "file://"+source)
	if err = command.Run(); err != nil {
		return nil, fmt.Errorf("render player report: %w", err)
	}
	image, err := os.ReadFile(output)
	if err != nil {
		return nil, err
	}
	if len(image) > 8<<20 {
		return nil, fmt.Errorf("report image exceeds 8 MiB")
	}
	return image, nil
}

func reportHeight(count int) int {
	height := 410 + count*90
	if height < 1150 {
		return 1150
	}
	return height
}
