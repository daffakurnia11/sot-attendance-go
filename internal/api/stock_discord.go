package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/daffakurniawan/sot-discord-bot/internal/stock"
)

type StockDiscordChannels struct {
	Public string
	Boss   string
}

type DiscordStockNotifier struct {
	client   *http.Client
	token    string
	channels StockDiscordChannels
	baseURL  string
}

func NewDiscordStockNotifier(client *http.Client, token string, channels StockDiscordChannels) *DiscordStockNotifier {
	return &DiscordStockNotifier{client: client, token: strings.TrimSpace(token), channels: channels, baseURL: "https://discord.com/api/v10"}
}

type stockNoticeKey struct{ safebox, action string }

func (n *DiscordStockNotifier) Notify(ctx context.Context, actorDiscordID string, movements []stock.Movement, names map[string]string) error {
	groups := make(map[stockNoticeKey][]stock.Movement)
	for _, movement := range movements {
		key := stockNoticeKey{movement.Safebox, movement.Action}
		groups[key] = append(groups[key], movement)
	}
	keys := make([]stockNoticeKey, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].safebox == keys[j].safebox {
			return keys[i].action < keys[j].action
		}
		return keys[i].safebox < keys[j].safebox
	})
	var failures []error
	for _, key := range keys {
		channelID := n.channelID(key)
		if channelID == "" {
			failures = append(failures, fmt.Errorf("missing channel for %s %s", key.safebox, key.action))
			continue
		}
		if err := n.send(ctx, channelID, actorDiscordID, key, groups[key], names); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

// Deposits and withdrawals share their safebox's channel; the embed title and
// colour are what tell them apart.
func (n *DiscordStockNotifier) channelID(key stockNoticeKey) string {
	if key.safebox == "boss" {
		return n.channels.Boss
	}
	return n.channels.Public
}

func stockMovementLines(movements []stock.Movement, names map[string]string) []string {
	lines := make([]string, 0, len(movements))
	for _, movement := range movements {
		name := strings.TrimSpace(names[movement.ItemKey])
		if name == "" {
			name = movement.ItemKey
		}
		lines = append(lines, fmt.Sprintf("**%s** × %s", name, formatStockNumber(int64(movement.Quantity))))
	}
	return lines
}

func (n *DiscordStockNotifier) send(ctx context.Context, channelID, actorDiscordID string, key stockNoticeKey, movements []stock.Movement, names map[string]string) error {
	lines := stockMovementLines(movements, names)
	title, color := "Deposit", 0x57F287
	if key.action == stock.ActionWithdraw {
		title, color = "Withdraw", 0xED4245
	}
	stashName := strings.ToUpper(key.safebox[:1]) + key.safebox[1:] + " Stash"
	description := "Purpose: Crafting Products"
	if key.action == stock.ActionWithdraw {
		description = "Purpose: Crafting Materials"
	}
	if actorDiscordID != "" {
		description += " · <@" + actorDiscordID + ">"
	}
	fields := []map[string]any{{"name": "Items", "value": boundedStockLines(lines, 1024)}}
	payload := map[string]any{
		"embeds": []map[string]any{{
			"title": title + " · " + stashName, "description": description, "color": color,
			"fields":    fields,
			"timestamp": time.Now().UTC().Format(time.RFC3339),
		}},
		"allowed_mentions": map[string]any{"parse": []string{}},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode Discord stock embed: %w", err)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, n.baseURL+"/channels/"+channelID+"/messages", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create Discord stock message: %w", err)
	}
	request.Header.Set("Authorization", "Bot "+n.token)
	request.Header.Set("Content-Type", "application/json")
	response, err := n.client.Do(request)
	if err != nil {
		return fmt.Errorf("send Discord stock message to %s: %w", channelID, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		return fmt.Errorf("send Discord stock message to %s: status %d", channelID, response.StatusCode)
	}
	return nil
}

func boundedStockLines(lines []string, limit int) string {
	var result strings.Builder
	for _, line := range lines {
		separator := ""
		if result.Len() > 0 {
			separator = "\n"
		}
		if result.Len()+len(separator)+len(line) > limit {
			if result.Len() == 0 {
				return line[:limit]
			}
			break
		}
		result.WriteString(separator)
		result.WriteString(line)
	}
	return result.String()
}

func formatStockNumber(value int64) string {
	digits := strconv.FormatInt(value, 10)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	return digits
}
