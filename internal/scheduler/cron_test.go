package scheduler

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testZone keeps every expectation independent of the host's local zone.
// 2026-10-21 in it is a Wednesday.
var testZone = time.FixedZone("UTC+7", 7*3600)

func at(day, hour, minute int) time.Time {
	return time.Date(2026, time.October, day, hour, minute, 0, 0, testZone)
}

func TestParseFiveField(t *testing.T) {
	tests := []struct {
		name    string
		expr    string
		wantErr []string // substrings the error must contain; empty means success
	}{
		{name: "every five minutes", expr: "*/5 * * * *"},
		{name: "weekday range", expr: "0 9 * * 1-5"},
		{name: "weekday list", expr: "30 17 * * 1,5"},
		{name: "monthly", expr: "0 0 1 * *"},
		{name: "day names", expr: "0 9 * * MON-FRI"},
		{
			name:    "not an expression",
			expr:    "every monday",
			wantErr: []string{fiveFieldHint, `got "every monday"`},
		},
		{
			name:    "descriptor",
			expr:    "@daily",
			wantErr: []string{fiveFieldHint, `got "@daily"`},
		},
		{
			name:    "six fields",
			expr:    "0 0 9 * * *",
			wantErr: []string{fiveFieldHint, `got "0 0 9 * * *"`},
		},
		{
			name:    "four fields",
			expr:    "0 9 * *",
			wantErr: []string{fiveFieldHint},
		},
		{
			name:    "hour out of range",
			expr:    "0 25 * * *",
			wantErr: []string{fiveFieldHint, "25"},
		},
		{
			name:    "minute out of range",
			expr:    "60 * * * *",
			wantErr: []string{fiveFieldHint, "60"},
		},
		{
			name:    "empty",
			expr:    "",
			wantErr: []string{fiveFieldHint},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			schedule, err := ParseFiveField(tt.expr)
			if len(tt.wantErr) == 0 {
				require.NoError(t, err)
				require.NotNil(t, schedule)
				return
			}
			require.Error(t, err)
			require.Nil(t, schedule)
			for _, want := range tt.wantErr {
				require.Contains(t, err.Error(), want)
			}
		})
	}
}

func TestNextN_DailyAtNine(t *testing.T) {
	nexts, err := NextN("0 9 * * *", at(21, 10, 0), 3) // Wednesday 10:00
	require.NoError(t, err)
	require.Equal(t, []time.Time{at(22, 9, 0), at(23, 9, 0), at(24, 9, 0)}, nexts)
}

func TestNextN_WeeklyMondayAndFriday(t *testing.T) {
	nexts, err := NextN("30 17 * * 1,5", at(21, 12, 0), 3) // Wednesday 12:00
	require.NoError(t, err)
	require.Equal(t, []time.Time{at(23, 17, 30), at(26, 17, 30), at(30, 17, 30)}, nexts)
}

func TestNextN_MinutelyQuarterHours(t *testing.T) {
	nexts, err := NextN("*/15 * * * *", at(21, 10, 2), 3)
	require.NoError(t, err)
	require.Equal(t, []time.Time{at(21, 10, 15), at(21, 10, 30), at(21, 10, 45)}, nexts)
}

func TestNext_IsStrictlyAfter(t *testing.T) {
	next, err := Next("0 9 * * *", at(21, 9, 0)) // exactly on a slot
	require.NoError(t, err)
	require.Equal(t, at(22, 9, 0), next)
}

func TestNext_InvalidExpression(t *testing.T) {
	_, err := Next("every monday", at(21, 9, 0))
	require.Error(t, err)
	require.Contains(t, err.Error(), fiveFieldHint)
}

func TestNextN_NonPositiveCount(t *testing.T) {
	nexts, err := NextN("0 9 * * *", at(21, 9, 0), 0)
	require.NoError(t, err)
	require.Empty(t, nexts)
}

func TestDescribe(t *testing.T) {
	tests := []struct {
		name string
		expr string
		want string
	}{
		{name: "every minute", expr: "* * * * *", want: "Every minute"},
		{name: "every N minutes", expr: "*/5 * * * *", want: "Every 5 minutes"},
		{name: "hourly", expr: "30 * * * *", want: "Every hour at :30"},
		{name: "every N hours", expr: "30 */6 * * *", want: "Every 6 hours at :30"},
		{name: "daily zero-pads the time", expr: "15 9 * * *", want: "Daily at 09:15"},
		{name: "weekday range", expr: "0 9 * * 1-5", want: "Every weekday at 09:00"},
		{name: "weekday full list", expr: "0 9 * * 1,2,3,4,5", want: "Every weekday at 09:00"},
		{name: "weekends", expr: "30 20 * * 0,6", want: "Weekends at 20:30"},
		{name: "weekends reversed", expr: "30 20 * * 6,0", want: "Weekends at 20:30"},
		{name: "two days", expr: "30 17 * * 1,5", want: "Weekly on Monday and Friday at 17:30"},
		{name: "single day", expr: "0 9 * * 3", want: "Weekly on Wednesday at 09:00"},
		{name: "monthly", expr: "0 9 1 * *", want: "Monthly on day 1 at 09:00"},
		{
			name: "custom fallback",
			expr: "*/15 9-17 * * 1-5",
			want: "Custom schedule (*/15 9-17 * * 1-5)",
		},
		{name: "invalid expression", expr: "every monday", want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, Describe(tt.expr))
		})
	}
}

func TestPreviewAt(t *testing.T) {
	now := at(21, 10, 0) // Wednesday 10:00

	t.Run("valid expression", func(t *testing.T) {
		p := PreviewAt("0 9 * * 1-5", now)
		require.True(t, p.OK)
		require.Empty(t, p.Error)
		require.Equal(t, "Every weekday at 09:00", p.Description)
		require.Equal(t, []time.Time{at(22, 9, 0), at(23, 9, 0), at(26, 9, 0)}, p.Next)
	})

	t.Run("invalid expression", func(t *testing.T) {
		p := PreviewAt("every monday", now)
		require.False(t, p.OK)
		require.Contains(t, p.Error, fiveFieldHint)
		require.Empty(t, p.Description)
		require.Empty(t, p.Next)
	})
}
