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

type ScheduleTimes struct {
	Start time.Duration
	End   time.Duration
}

type scheduleLoader func(context.Context) (ScheduleTimes, error)

type Scheduler struct {
	channelID  string
	serverName string
	location   *time.Location
	schedules  []attendanceSchedule
	loadTimes  scheduleLoader
	refresh    time.Duration
	logger     *slog.Logger
	endAction  func(context.Context, *discordgo.Session, time.Time) error
}

func NewScheduler(channelID, serverName string, startTime, endTime time.Duration, endAction func(context.Context, *discordgo.Session, time.Time) error, logger *slog.Logger) (*Scheduler, error) {
	scheduler, err := NewDynamicScheduler(channelID, serverName, func(context.Context) (ScheduleTimes, error) {
		return ScheduleTimes{Start: startTime, End: endTime}, nil
	}, endAction, logger)
	if err != nil {
		return nil, err
	}
	scheduler.schedules = schedulesFor(ScheduleTimes{Start: startTime, End: endTime})
	return scheduler, nil
}

func NewDynamicScheduler(channelID, serverName string, loadTimes scheduleLoader, endAction func(context.Context, *discordgo.Session, time.Time) error, logger *slog.Logger) (*Scheduler, error) {
	location, err := time.LoadLocation("Asia/Jakarta")
	if err != nil {
		return nil, fmt.Errorf("load Asia/Jakarta timezone: %w", err)
	}
	return &Scheduler{
		channelID:  channelID,
		serverName: serverName,
		location:   location,
		loadTimes:  loadTimes,
		refresh:    30 * time.Second,
		logger:     logger,
		endAction:  endAction,
	}, nil
}

func (s *Scheduler) Run(ctx context.Context, session *discordgo.Session) {
	for {
		times, err := s.loadTimes(ctx)
		if err != nil {
			s.logger.Error("load attendance schedule", "error", err)
			if !waitFor(ctx, s.refresh) {
				return
			}
			continue
		}
		s.schedules = schedulesFor(times)
		now := time.Now()
		schedule, runAt := s.next(now)
		s.logger.Info("attendance announcement scheduled", "command", schedule.command, "channel_id", s.channelID, "run_at", runAt.In(s.location).Format(time.RFC3339), "timezone", s.location.String())
		wait := time.Until(runAt)
		if wait > s.refresh {
			wait = s.refresh
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return
		case announcedAt := <-timer.C:
			if announcedAt.Before(runAt) {
				continue
			}
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

func schedulesFor(times ScheduleTimes) []attendanceSchedule {
	return []attendanceSchedule{
		{command: commandattendance.AttendanceStart, timeOfDay: times.Start},
		{command: commandattendance.AttendanceEnd, timeOfDay: times.End},
	}
}

func waitFor(ctx context.Context, duration time.Duration) bool {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
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
