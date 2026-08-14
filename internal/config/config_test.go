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
		token                string
		guildID              string
		serverName           string
		pollInterval         string
		commandPrefix        string
		playerLogChannelID   string
		blacklistedUsers     string
		playerChatChannelID  string
		playerRecapChannelID string
		attendanceStartTime  string
		attendanceEndTime    string
		attendancePlaytime   string
		want                 Config
		wantErr              bool
	}{
		{
			name:                 "valid",
			token:                " token ",
			guildID:              " 123456 ",
			serverName:           " CR Roleplay ",
			pollInterval:         " 5000 ",
			commandPrefix:        " $ ",
			playerLogChannelID:   " 789012 ",
			blacklistedUsers:     " 111, 222,111 ",
			playerChatChannelID:  " 345678 ",
			playerRecapChannelID: " 901234 ",
			attendanceStartTime:  "21:00",
			attendanceEndTime:    "01:00",
			attendancePlaytime:   "90m",
			want: Config{
				Token: "token", GuildID: "123456", ServerName: "CR Roleplay",
				PollInterval: 5 * time.Second, CommandPrefix: "$", PlayerLogChannelID: "789012",
				BlacklistedUserIDs:  []string{"111", "222"},
				PlayerChatChannelID: "345678", AttendanceStartTime: 21 * time.Hour, AttendanceEndTime: time.Hour,
				PlayerRecapChannelID: "901234",
				AttendancePlaytime:   90 * time.Minute, DatabaseURL: "postgres://test",
			},
		},
		{name: "missing token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing guild", token: "token", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "invalid guild", token: "token", guildID: "12abc", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing server name", token: "token", guildID: "123", pollInterval: "1000", playerLogChannelID: "456", wantErr: true},
		{name: "missing poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", playerLogChannelID: "456", wantErr: true},
		{name: "zero poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "0", playerLogChannelID: "456", wantErr: true},
		{name: "negative poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "-1", playerLogChannelID: "456", wantErr: true},
		{name: "invalid poll interval", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1s", playerLogChannelID: "456", wantErr: true},
		{name: "default command prefix", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", want: Config{Token: "token", GuildID: "123", ServerName: "CR Roleplay", PollInterval: time.Second, CommandPrefix: "!", PlayerLogChannelID: "456", PlayerChatChannelID: "789", PlayerRecapChannelID: "987", AttendanceStartTime: 21 * time.Hour, AttendanceEndTime: time.Hour, AttendancePlaytime: 90 * time.Minute, DatabaseURL: "postgres://test"}},
		{name: "invalid command prefix", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", commandPrefix: "! me", playerLogChannelID: "456", wantErr: true},
		{name: "missing log channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", wantErr: true},
		{name: "invalid log channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "abc", wantErr: true},
		{name: "invalid blacklisted user", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", blacklistedUsers: "123,abc", wantErr: true},
		{name: "empty blacklisted entry", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", blacklistedUsers: "123,,456", wantErr: true},
		{name: "missing player chat channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", attendanceStartTime: "21:00", attendanceEndTime: "01:00", wantErr: true},
		{name: "missing player recap channel", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", playerChatChannelID: "789", attendanceStartTime: "21:00", attendanceEndTime: "01:00", attendancePlaytime: "90m", wantErr: true},
		{name: "invalid attendance start", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", playerChatChannelID: "789", attendanceStartTime: "9pm", attendanceEndTime: "01:00", wantErr: true},
		{name: "same attendance times", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", playerChatChannelID: "789", attendanceStartTime: "21:00", attendanceEndTime: "21:00", wantErr: true},
		{name: "missing attendance playtime", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", playerChatChannelID: "789", attendanceStartTime: "21:00", attendanceEndTime: "01:00", wantErr: true},
		{name: "invalid attendance playtime", token: "token", guildID: "123", serverName: "CR Roleplay", pollInterval: "1000", playerLogChannelID: "456", playerChatChannelID: "789", attendanceStartTime: "21:00", attendanceEndTime: "01:00", attendancePlaytime: "90", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if tt.playerChatChannelID == "" && !tt.wantErr {
				tt.playerChatChannelID = "789"
			}
			if tt.playerRecapChannelID == "" && !tt.wantErr {
				tt.playerRecapChannelID = "987"
			}
			if tt.attendanceStartTime == "" && !tt.wantErr {
				tt.attendanceStartTime = "21:00"
			}
			if tt.attendanceEndTime == "" && !tt.wantErr {
				tt.attendanceEndTime = "01:00"
			}
			if tt.attendancePlaytime == "" && !tt.wantErr {
				tt.attendancePlaytime = "90m"
			}
			got, err := FromValues(tt.token, tt.guildID, tt.serverName, tt.pollInterval, tt.commandPrefix, tt.playerLogChannelID, tt.blacklistedUsers, tt.playerChatChannelID, tt.playerRecapChannelID, tt.attendanceStartTime, tt.attendanceEndTime, tt.attendancePlaytime, "postgres://test")
			if (err != nil) != tt.wantErr {
				t.Fatalf("FromValues() error = %v, wantErr %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FromValues() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
