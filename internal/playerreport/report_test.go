package playerreport

import (
	"bytes"
	"context"
	"github.com/daffakurniawan/sot-discord-bot/internal/dashboard"
	"image/png"
	"os"
	"strings"
	"testing"
	"time"
	_ "time/tzdata"
)

func TestCaptureMatchesPlayerLogsRoster(t *testing.T) {
	id := int64(1)
	slot := "45"
	empty := ""
	at := time.Date(2026, 10, 22, 15, 0, 0, 0, time.UTC)
	players := []dashboard.Player{
		{MemberID: &id, DiscordUserID: "123", CharacterName: "Kenji", CID: "A", Status: "connected", ServerID: &slot},
		{MemberID: &id, DiscordUserID: "123", CharacterName: "Another character", CID: "B", Status: "connected"},
		{CharacterName: "Guest", CID: "C", Status: "connected", ServerID: &empty},
		{CharacterName: "Arriving", CID: "D", Status: "connecting"},
		{CharacterName: "Offline", CID: "E", Status: "offline", DiscordPlaying: true},
	}
	snapshot := Capture(at, players)
	if len(snapshot.Players) != 4 || snapshot.ConnectedCount() != 3 || !snapshot.At.Equal(at) {
		t.Fatalf("wrong roster: %+v", snapshot)
	}
	want := []string{"Another character", "Guest", "Kenji", "Arriving"}
	for i, name := range want {
		if snapshot.Players[i].Name != name {
			t.Fatalf("row %d: got %s want %s", i, snapshot.Players[i].Name, name)
		}
	}
	if snapshot.Players[0].ServerID != "" || snapshot.Players[1].ServerID != "" {
		t.Fatal("missing slots must remain optional")
	}
	players[0].CharacterName = "Changed"
	if snapshot.Players[2].Name != "Kenji" {
		t.Fatal("snapshot not frozen")
	}
}
func TestWindowAndCaption(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	for _, tc := range []struct {
		clock  string
		active bool
	}{{"20:59:59", false}, {"21:00:00", true}, {"23:59:59", true}, {"00:00:00", false}} {
		at, _ := time.ParseInLocation("2006-01-02 15:04:05", "2026-10-22 "+tc.clock, loc)
		date, active := WindowDate(at.UTC(), loc)
		if date != "2026-10-22" || active != tc.active {
			t.Fatalf("bad window %s", tc.clock)
		}
	}
	s := Snapshot{At: time.Date(2026, 10, 22, 16, 8, 0, 0, time.UTC), Players: []Player{{Name: "Kenji"}}}
	text := Caption(s, true, loc)
	if !strings.HasPrefix(text, "```") || strings.Contains(text, "Current server presence") || !strings.Contains(text, "23:08:00 WIB") || !strings.Contains(text, "1 Orang") || !strings.Contains(text, "22/10/2026") {
		t.Fatal(text)
	}
	html, err := HTML(Snapshot{At: s.At, Players: []Player{{Name: "<script>alert(1)</script>"}}}, loc)
	if err != nil || bytes.Contains(html, []byte("<script>")) || !bytes.Contains(html, []byte("&lt;script&gt;")) {
		t.Fatal("report HTML not escaped")
	}
}
func TestRenderPNG(t *testing.T) {
	if os.Getenv("PLAYER_REPORT_RENDER_TEST") != "1" {
		t.Skip("requires isolated Chromium test image")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	loc, _ := time.LoadLocation("Asia/Jakarta")
	data, err := Render(ctx, Snapshot{At: time.Now(), Players: []Player{{Name: "Kenji Nakamura", Username: "deltakilo11", ServerID: "613"}}}, loc)
	if err != nil {
		t.Fatal(err)
	}
	if path := os.Getenv("PLAYER_REPORT_PREVIEW_PATH"); path != "" {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil || image.Bounds().Dx() != 1200 {
		t.Fatalf("invalid PNG: %v", err)
	}
}

func TestConnectingRowsDoNotCountAsPeak(t *testing.T) {
	loc, _ := time.LoadLocation("Asia/Jakarta")
	s := Snapshot{At: time.Now(), Players: []Player{{Name: "Connected", ServerStatus: "connected"}, {Name: "Joining", ServerStatus: "connecting"}}}
	if s.ConnectedCount() != 1 || !strings.Contains(Caption(s, true, loc), "2 Orang") {
		t.Fatal("peak must count connected only; caption must count all rows")
	}
	html, err := HTML(s, loc)
	if err != nil || !bytes.Contains(html, []byte("CONNECTING")) || bytes.Contains(html, []byte("SNAPSHOT")) {
		t.Fatal("incorrect report", err)
	}
	if (Snapshot{Players: []Player{{Name: "Legacy"}}}).ConnectedCount() != 1 {
		t.Fatal("legacy snapshot count changed")
	}
}
