package attendance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/bwmarrin/discordgo"
	commandattendance "github.com/daffakurniawan/sot-discord-bot/internal/command/attendance"
)

type attendanceSchedule struct {
	command   string
	timeOfDay time.Duration
}

type Scheduler struct {
	channelID  string
	serverName string
	location   *time.Location
	schedules  []attendanceSchedule
	logger     *slog.Logger
	endAction  func(context.Context, *discordgo.Session, time.Time) error
}

func NewScheduler(channelID, serverName string, startTime, endTime time.Duration, endAction func(context.Context, *discordgo.Session, time.Time) error, logger *slog.Logger) (*Scheduler, error) {
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return nil, fmt.Errorf("load Asia/Jakarta timezone: %w", err)
	}
	return &Scheduler{
		channelID:  channelID,
		serverName: serverName,
		location:   location,
		schedules: []attendanceSchedule{
			{command: commandattendance.AttendanceStart, timeOfDay: startTime},
			{command: commandattendance.AttendanceEnd, timeOfDay: endTime},
		},
		logger:    logger,
		endAction: endAction,
	}, nil
}

func (s *Scheduler) Run(ctx context.Context, session *discordgo.Session) {
	for {
		now := time.Now()
		schedule, runAt := s.next(now)
		s.logger.Info("attendance announcement scheduled", "command", schedule.command, "channel_id", s.channelID, "run_at", runAt.In(s.location).Format(time.RFC3339), "timezone", s.location.String())
		timer := time.NewTimer(time.Until(runAt))
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case announcedAt := <-timer.C:
			s.logger.Info("attendance schedule fired", "command", schedule.command, "scheduled_at", runAt.In(s.location).Format(time.RFC3339))
			if schedule.command == commandattendance.AttendanceEnd {
				go s.sendClosing(session, announcedAt, runAt)
				go s.sendRecap(ctx, session, announcedAt, runAt)
				continue
			}
			message := commandattendance.Announcement(schedule.command, s.serverName, announcedAt)
			if _, err := session.ChannelMessageSendComplex(s.channelID, message); err != nil {
				s.logger.Error("send scheduled attendance announcement", "command", schedule.command, "channel_id", s.channelID, "error", err)
				continue
			}
			s.logger.Info("scheduled attendance announced", "command", schedule.command, "channel_id", s.channelID, "scheduled_at", runAt.In(s.location).Format(time.RFC3339))
		}
	}
}

func (s *Scheduler) sendClosing(session *discordgo.Session, announcedAt, scheduledAt time.Time) {
	message := commandattendance.Announcement(commandattendance.AttendanceEnd, s.serverName, announcedAt)
	if _, err := session.ChannelMessageSendComplex(s.channelID, message); err != nil {
		s.logger.Error("send scheduled attendance closing announcement", "channel_id", s.channelID, "error", err)
		return
	}
	s.logger.Info("scheduled attendance closing announced", "channel_id", s.channelID, "scheduled_at", scheduledAt.In(s.location).Format(time.RFC3339))
}

func (s *Scheduler) sendRecap(ctx context.Context, session *discordgo.Session, announcedAt, scheduledAt time.Time) {
	if s.endAction == nil {
		s.logger.Error("scheduled attendance recap unavailable")
		return
	}
	if err := s.endAction(ctx, session, announcedAt); err != nil {
		s.logger.Error("send scheduled attendance recap", "error", err)
		return
	}
	s.logger.Info("scheduled attendance recap sent", "scheduled_at", scheduledAt.In(s.location).Format(time.RFC3339))
}

func (s *Scheduler) next(now time.Time) (attendanceSchedule, time.Time) {
	now = now.In(s.location)
	nextSchedule := s.schedules[0]
	nextRun := nextDailyRun(now, nextSchedule.timeOfDay, s.location)
	for _, schedule := range s.schedules[1:] {
		runAt := nextDailyRun(now, schedule.timeOfDay, s.location)
		if runAt.Before(nextRun) {
			nextSchedule = schedule
			nextRun = runAt
		}
	}
	return nextSchedule, nextRun
}

func nextDailyRun(now time.Time, timeOfDay time.Duration, location *time.Location) time.Time {
	localNow := now.In(location)
	hour := int(timeOfDay / time.Hour)
	minute := int(timeOfDay%time.Hour) / int(time.Minute)
	runAt := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, location)
	if !runAt.After(localNow) {
		runAt = runAt.AddDate(0, 0, 1)
	}
	return runAt
}
