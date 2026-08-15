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
		{name: "valid", values: []string{"21:00", "01:00", "90m", "15", "8000000", "24", "30", "28"}, want: Attendance{StartTime: 21 * time.Hour, EndTime: time.Hour, PlaytimeThreshold: 90 * time.Minute}},
		{name: "trim values", values: []string{" 08:30 ", " 17:45 ", " 1h30m ", "15", "8000000", "24", "30", "28"}, want: Attendance{StartTime: 8*time.Hour + 30*time.Minute, EndTime: 17*time.Hour + 45*time.Minute, PlaytimeThreshold: 90 * time.Minute}},
		{name: "missing start", values: []string{"", "01:00", "90m", "15", "8000000", "24", "30", "28"}, wantErr: "setting start_attendance"},
		{name: "invalid end", values: []string{"21:00", "1am", "90m", "15", "8000000", "24", "30", "28"}, wantErr: "setting end_attendance"},
		{name: "same times", values: []string{"21:00", "21:00", "90m", "15", "8000000", "24", "30", "28"}, wantErr: "must differ"},
		{name: "missing playtime", values: []string{"21:00", "01:00", "", "15", "8000000", "24", "30", "28"}, wantErr: "setting playtime_threshold"},
		{name: "non-positive playtime", values: []string{"21:00", "01:00", "0m", "15", "8000000", "24", "30", "28"}, wantErr: "setting playtime_threshold"},
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

func TestValidateAttendanceDayRange(t *testing.T) {
	values := Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "30", AttendanceMaximum: "24", StartDateContract: "28"}
	if _, err := Validate(values); err == nil || !strings.Contains(err.Error(), "must not exceed") {
		t.Fatalf("Validate() error = %v", err)
	}
}

func TestValidateContractStartDate(t *testing.T) {
	values := Values{StartAttendance: "21:00", EndAttendance: "01:00", PlaytimeThreshold: "90m", PlayerThreshold: "15", PaymentContract: "8000000", AttendanceMinimum: "1", AttendanceMaximum: "30", StartDateContract: "32"}
	if _, err := Validate(values); err == nil || !strings.Contains(err.Error(), "start_date_contract") {
		t.Fatalf("Validate() error = %v", err)
	}
}
