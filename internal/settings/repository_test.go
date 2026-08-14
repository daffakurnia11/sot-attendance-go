package settings

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type fakeDatabase struct{ row pgx.Row }

func (f fakeDatabase) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }

type fakeRow struct {
	values []string
	err    error
}

func (r fakeRow) Scan(destinations ...any) error {
	if r.err != nil {
		return r.err
	}
	for index, value := range r.values {
		*destinations[index].(*string) = value
	}
	return nil
}

func TestRepositoryLoadAttendance(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		values  []string
		rowErr  error
		want    Attendance
		wantErr string
	}{
		{name: "valid", values: []string{"21:00", "01:00", "90m"}, want: Attendance{StartTime: 21 * time.Hour, EndTime: time.Hour, PlaytimeThreshold: 90 * time.Minute}},
		{name: "trim values", values: []string{" 08:30 ", " 17:45 ", " 1h30m "}, want: Attendance{StartTime: 8*time.Hour + 30*time.Minute, EndTime: 17*time.Hour + 45*time.Minute, PlaytimeThreshold: 90 * time.Minute}},
		{name: "missing start", values: []string{"", "01:00", "90m"}, wantErr: "setting start_attendance"},
		{name: "invalid end", values: []string{"21:00", "1am", "90m"}, wantErr: "setting end_attendance"},
		{name: "same times", values: []string{"21:00", "21:00", "90m"}, wantErr: "must differ"},
		{name: "missing playtime", values: []string{"21:00", "01:00", ""}, wantErr: "setting playtime_threshold"},
		{name: "non-positive playtime", values: []string{"21:00", "01:00", "0m"}, wantErr: "setting playtime_threshold"},
		{name: "query failure", rowErr: errors.New("database unavailable"), wantErr: "load attendance settings"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := NewRepository(fakeDatabase{row: fakeRow{values: test.values, err: test.rowErr}}).LoadAttendance(context.Background())
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("LoadAttendance() error = %v, want containing %q", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("LoadAttendance() error = %v", err)
			}
			if !reflect.DeepEqual(got, test.want) {
				t.Errorf("LoadAttendance() = %#v, want %#v", got, test.want)
			}
		})
	}
}
