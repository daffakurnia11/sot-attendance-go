package attendance

import (
	"io"
	"log/slog"
	"testing"
	"time"

	commandattendance "github.com/daffakurniawan/sot-discord-bot/internal/command/attendance"
)

func TestAttendanceSchedulerNextOvernight(t *testing.T) {
	t.Parallel()

	scheduler, err := NewScheduler("channel", "CR Roleplay", 21*time.Hour, time.Hour, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		now         string
		wantCommand string
		wantRun     string
	}{
		{name: "before start", now: "2026-08-13T20:59:00+07:00", wantCommand: commandattendance.AttendanceStart, wantRun: "2026-08-13T21:00:00+07:00"},
		{name: "after start before end", now: "2026-08-13T23:00:00+07:00", wantCommand: commandattendance.AttendanceEnd, wantRun: "2026-08-14T01:00:00+07:00"},
		{name: "after end", now: "2026-08-14T02:00:00+07:00", wantCommand: commandattendance.AttendanceStart, wantRun: "2026-08-14T21:00:00+07:00"},
		{name: "exact time moves next day", now: "2026-08-13T21:00:00+07:00", wantCommand: commandattendance.AttendanceEnd, wantRun: "2026-08-14T01:00:00+07:00"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			now, err := time.Parse(time.RFC3339, tt.now)
			if err != nil {
				t.Fatal(err)
			}
			schedule, runAt := scheduler.next(now)
			if schedule.command != tt.wantCommand || runAt.Format(time.RFC3339) != tt.wantRun {
				t.Errorf("next() = %s at %s, want %s at %s", schedule.command, runAt.Format(time.RFC3339), tt.wantCommand, tt.wantRun)
			}
		})
	}
}
