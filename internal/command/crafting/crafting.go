package crafting

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bwmarrin/discordgo"
	craftingdomain "github.com/daffakurniawan/sot-discord-bot/internal/crafting"
)

const Command = "craft"

var ErrInvalidSyntax = errors.New("invalid craft command syntax")

func Parse(content, prefix string) ([]craftingdomain.BatchItem, error) {
	parts := strings.Fields(strings.TrimSpace(content))
	if len(parts) < 2 || parts[0] != prefix+Command || len(parts)-1 > craftingdomain.MaxRecipes {
		return nil, ErrInvalidSyntax
	}
	items := make([]craftingdomain.BatchItem, 0, len(parts)-1)
	for _, part := range parts[1:] {
		pair := strings.Split(part, ":")
		if len(pair) != 2 {
			return nil, ErrInvalidSyntax
		}
		weaponCode := normalizeCode(pair[0])
		quantity, err := strconv.ParseInt(pair[1], 10, 64)
		if weaponCode == "" || err != nil || quantity < 1 || quantity > craftingdomain.MaxQuantity {
			return nil, ErrInvalidSyntax
		}
		items = append(items, craftingdomain.BatchItem{WeaponCode: weaponCode, Quantity: quantity})
	}
	return items, nil
}

func Usage(prefix string) string {
	return fmt.Sprintf("Usage: `%scraft vector:30 mp9:20 crx_mk2:5`", prefix)
}

func Embed(result craftingdomain.BatchCalculation) *discordgo.MessageEmbed {
	products := make([]string, 0, len(result.Recipes))
	for _, recipe := range result.Recipes {
		products = append(products, fmt.Sprintf("**%s** × %s", recipe.WeaponName, formatNumber(recipe.RequestedQuantity)))
	}
	materials := make([]string, 0, len(result.Ingredients))
	for _, ingredient := range result.Ingredients {
		materials = append(materials, fmt.Sprintf("**%s** — %s", ingredient.ItemName, formatNumber(ingredient.TotalQuantity)))
	}
	return &discordgo.MessageEmbed{
		Title: "Crafting Plan",
		Color: 0xF2B63D,
		Fields: []*discordgo.MessageEmbedField{
			{Name: "Products", Value: boundedLines(products, 1024), Inline: false},
			{Name: "Total Materials", Value: boundedLines(materials, 1024), Inline: false},
			{Name: "Summary", Value: fmt.Sprintf("Weapons: **%s** · Crafts: **%s** · Time: **%s**", formatNumber(result.TotalRequestedQuantity), formatNumber(result.TotalCraftCount), formatDuration(result.TotalCraftingTimeSeconds)), Inline: false},
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

func normalizeCode(value string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(value)), "-", "_")
}

func formatNumber(value int64) string {
	digits := strconv.FormatInt(value, 10)
	for index := len(digits) - 3; index > 0; index -= 3 {
		digits = digits[:index] + "," + digits[index:]
	}
	return digits
}

func formatDuration(seconds int64) string {
	duration := time.Duration(seconds) * time.Second
	hours := int64(duration / time.Hour)
	minutes := int64(duration%time.Hour) / int64(time.Minute)
	remainingSeconds := int64(duration%time.Minute) / int64(time.Second)
	parts := make([]string, 0, 3)
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
	}
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
	}
	if remainingSeconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", remainingSeconds))
	}
	return strings.Join(parts, " ")
}

func boundedLines(lines []string, limit int) string {
	if len(lines) == 0 {
		return "None"
	}
	var builder strings.Builder
	for index, line := range lines {
		separator := ""
		if builder.Len() > 0 {
			separator = "\n"
		}
		remaining := len(lines) - index
		suffix := fmt.Sprintf("\n…and %d more", remaining)
		if builder.Len()+len(separator)+len(line) > limit {
			if builder.Len()+len(suffix) <= limit {
				builder.WriteString(suffix)
			}
			break
		}
		builder.WriteString(separator)
		builder.WriteString(line)
	}
	return builder.String()
}
