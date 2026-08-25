package config

import (
	"reflect"
	"testing"
	"time"
)

func TestFromValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                 string
		appEnv               string
		discordRoleID        string
		discordAdminIDs      string
		token                string
		guildID              string
		serverName           string
		pollInterval         string
		commandPrefix        string
		playerLogChannelID   string
		blacklistedUsers     string
		playerChatChannelID  string
		playerRecapChannelID string
		want                 Config
		wantErr              bool
	}{
		{
			name:                 "valid",
			appEnv:               " local ",
			discordRoleID:        "123456789",
			token:                " token ",
			guildID:              " 123456 ",
			serverName:           " CR Roleplay ",
			pollInterval:         " 5000 ",
			commandPrefix:        " $ ",
			playerLogChannelID:   " 789012 ",
			blacklistedUsers:     " 111, 222,111 ",
			discordAdminIDs:      " 333,444,333 ",
			playerChatChannelID:  " 345678 ",
			playerRecapChannelID: " 901234 ",
			want: Config{
				AppEnv: "local",
				Token:  "token", GuildID: "123456", DiscordMemberRoleID: "123456789", ServerName: "CR Roleplay",
				PollInterval: 5 * time.Second, CommandPrefix: "$", PlayerLogChannelID: "789012",
				BlacklistedUserIDs:   []string{"111", "222"},
				DiscordAdminRoleIDs:  []string{"333", "444"},
				PlayerChatChannelID:  "345678",
				PlayerRecapChannelID: "901234",
				DatabaseURL:          "postgres://test",
			},
		},
		{
			name: "valid production", appEnv: " PRODUCTION ", discordRoleID: " 555666 ",
			token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456",
			want: Config{
				AppEnv: "production", DiscordRoleID: "555666", DiscordMemberRoleID: "555666", Token: "token", GuildID: "123", ServerName: "CR Roleplay",
				PollInterval: time.Second, CommandPrefix: "!", PlayerLogChannelID: "456", PlayerChatChannelID: "789",
				PlayerRecapChannelID: "987", DatabaseURL: "postgres://test",
			},
		},
		{name: "missing app env", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "invalid app env", appEnv: "staging", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "production missing role", appEnv: "production", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "production invalid role", appEnv: "production", discordRoleID: "role", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing guild", token: "token", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "invalid guild", token: "token", guildID: "12abc", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing server name", token: "token", guildID: "123", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", playerLogChannelID: "456", wantErr: true},
		{name: "zero poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "0", playerLogChannelID: "456", wantErr: true},
		{name: "negative poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "-1", playerLogChannelID: "456", wantErr: true},
		{name: "invalid poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1s", playerLogChannelID: "456", wantErr: true},
		{name: "default command prefix", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", want: Config{AppEnv: "local", Token: "token", GuildID: "123", ServerName: "CR Roleplay", PollInterval: time.Second, CommandPrefix: "!", PlayerLogChannelID: "456", PlayerChatChannelID: "789", PlayerRecapChannelID: "987", DatabaseURL: "postgres://test"}},
		{name: "invalid command prefix", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", commandPrefix: "! me", playerLogChannelID: "456", wantErr: true},
		{name: "missing log channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", wantErr: true},
		{name: "invalid log channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "abc", wantErr: true},
		{name: "invalid blacklisted user", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", blacklistedUsers: "123,abc", wantErr: true},
		{name: "invalid admin role", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", discordAdminIDs: "123,role", wantErr: true},
		{name: "empty blacklisted entry", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", blacklistedUsers: "123,,456", wantErr: true},
		{name: "missing player chat channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing player recap channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", playerChatChannelID: "789", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.appEnv == "" && tt.name != "missing app env" {
				tt.appEnv = "local"
			}
			if tt.playerChatChannelID == "" && !tt.wantErr {
				tt.playerChatChannelID = "789"
			}
			if tt.playerRecapChannelID == "" && !tt.wantErr {
				tt.playerRecapChannelID = "987"
			}
			got, err := FromValues(tt.token, tt.guildID, tt.serverName, tt.pollInterval, tt.commandPrefix, tt.playerLogChannelID, tt.blacklistedUsers, tt.playerChatChannelID, tt.playerRecapChannelID, "postgres://test", tt.appEnv, tt.discordRoleID, tt.discordAdminIDs)
			if (err != nil) != tt.wantErr {
				t.Fatalf("FromValues() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FromValues() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestWithStatusPolling(t *testing.T) {
	t.Parallel()

	configured, err := withStatusPolling(Config{}, " kr7k7d ", " SOT ", "60000", "2500")
	if err != nil {
		t.Fatal(err)
	}
	if configured.CFXServerID != "kr7k7d" || configured.CFXPlayerID != "SOT" || configured.CFXPollInterval != time.Minute || configured.StatusPollInterval != 2500*time.Millisecond {
		t.Fatalf("withStatusPolling() = %#v", configured)
	}

	invalid := []struct {
		name       string
		serverID   string
		playerID   string
		cfxPoll    string
		statusPoll string
	}{
		{name: "missing server", playerID: "SOT", cfxPoll: "60000", statusPoll: "2500"},
		{name: "invalid server", serverID: "kr7k7d/path", playerID: "SOT", cfxPoll: "60000", statusPoll: "2500"},
		{name: "missing player filter", serverID: "kr7k7d", cfxPoll: "60000", statusPoll: "2500"},
		{name: "invalid CFX poll", serverID: "kr7k7d", playerID: "SOT", cfxPoll: "0", statusPoll: "2500"},
		{name: "invalid status poll", serverID: "kr7k7d", playerID: "SOT", cfxPoll: "60000", statusPoll: "nope"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if _, err := withStatusPolling(Config{}, test.serverID, test.playerID, test.cfxPoll, test.statusPoll); err == nil {
				t.Fatal("withStatusPolling() error = nil")
			}
		})
	}
}

func TestWithMoneyChannels(t *testing.T) {
	t.Parallel()
	configured, err := withMoneyChannels(Config{}, " 123 ", " 456 ")
	if err != nil || configured.OfficeMoneyChannelID != "123" || configured.DirtyMoneyChannelID != "456" {
		t.Fatalf("withMoneyChannels() = %#v, %v", configured, err)
	}
	for _, channels := range [][2]string{{"", "456"}, {"abc", "456"}, {"123", ""}, {"123", "dirty"}, {"123", "123"}} {
		if _, err := withMoneyChannels(Config{}, channels[0], channels[1]); err == nil {
			t.Errorf("withMoneyChannels(%q, %q) error = nil", channels[0], channels[1])
		}
	}
}
