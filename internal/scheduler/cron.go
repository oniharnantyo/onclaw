package scheduler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// fiveFieldHint is the leading phrase every validation error carries, so the
// editor, the tool, and the service show one consistent, teaching message.
const fiveFieldHint = `recurrence must be a five-field cron expression ` +
	`"minute hour day-of-month month day-of-week" (for example "0 9 * * 1-5")`

// parser accepts exactly the five standard fields (minute, hour, day-of-month,
// month, day-of-week) with names, ranges, lists, and steps — no @descriptors,
// no seconds field, no timezone prefixes.
var parser = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)

// ParseFiveField validates that expr is exactly five whitespace-separated
// fields (minute hour day-of-month month day-of-week; names, ranges, lists
// and steps as the standard cron grammar accepts them) and parses it.
// Descriptors such as "@daily" and six-field (seconds) expressions are
// rejected before parsing. Errors lead with the expected form, e.g.:
//
//	recurrence must be a five-field cron expression "minute hour day-of-month month day-of-week" (for example "0 9 * * 1-5"); got "every monday"
//
// A five-field expression the grammar itself rejects (e.g. "0 25 * * *")
// wraps the library's error with the same leading phrase.
func ParseFiveField(expr string) (cron.Schedule, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 || strings.HasPrefix(fields[0], "@") {
		return nil, fmt.Errorf("%s; got %q", fiveFieldHint, expr)
	}
	schedule, err := parser.Parse(expr)
	if err != nil {
		return nil, fmt.Errorf("%s; %v", fiveFieldHint, err)
	}
	return schedule, nil
}

// Next returns the first fire strictly after after, evaluated in after's
// location: callers pass local time, and the schedule — which carries no
// explicit zone — computes in the location of the time it is given, so
// daylight-saving transitions are handled by the time package.
func Next(expr string, after time.Time) (time.Time, error) {
	schedule, err := ParseFiveField(expr)
	if err != nil {
		return time.Time{}, err
	}
	return schedule.Next(after), nil
}

// NextN returns up to n successive fires strictly after after, in after's
// location. n <= 0 yields an empty slice.
func NextN(expr string, after time.Time, n int) ([]time.Time, error) {
	if n <= 0 {
		return nil, nil
	}
	schedule, err := ParseFiveField(expr)
	if err != nil {
		return nil, err
	}
	fires := make([]time.Time, 0, n)
	t := after
	for i := 0; i < n; i++ {
		t = schedule.Next(t)
		fires = append(fires, t)
	}
	return fires, nil
}

// dayNames maps cron day-of-week numbers to names (0 = Sunday, 6 = Saturday).
var dayNames = [...]string{
	"Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday",
}

// Describe returns a human-readable sentence for the preset recurrence shapes
// and "Custom schedule (<expr>)" for any other valid expression. It returns ""
// for an invalid expression.
func Describe(expr string) string {
	if _, err := ParseFiveField(expr); err != nil {
		return ""
	}
	fields := strings.Fields(expr)
	minute, minOK := plainNumber(fields[0])
	hour, hourOK := plainNumber(fields[1])
	dom, domOK := plainNumber(fields[2])
	at := fmt.Sprintf("%02d:%02d", hour, minute)

	if fields[3] != "*" { // every preset leaves month as "*"
		return customDescription(expr)
	}
	switch {
	case fields[2] == "*" && fields[4] == "*":
		if d := describeDailyish(fields, minute, minOK, hour, hourOK, at); d != "" {
			return d
		}
	case fields[2] == "*" && minOK && hourOK:
		if d := describeWeekly(fields[4], at); d != "" {
			return d
		}
	case fields[4] == "*" && domOK && minOK && hourOK:
		return fmt.Sprintf("Monthly on day %d at %s", dom, at)
	}
	return customDescription(expr)
}

// describeDailyish names the minute/hour-only shapes: "* * * * *" and its
// stepped, hourly, and daily variants over "* * <dom> <month> *".
func describeDailyish(fields []string, minute int, minOK bool, hour int, hourOK bool, at string) string {
	switch {
	case fields[0] == "*" && fields[1] == "*":
		return "Every minute"
	case fields[1] == "*":
		if n, ok := stepNumber(fields[0]); ok {
			return everyMinutes(n)
		}
		if minOK {
			return fmt.Sprintf("Every hour at :%02d", minute)
		}
	case minOK:
		if n, ok := stepNumber(fields[1]); ok {
			return everyHours(n, minute)
		}
		if hourOK {
			return fmt.Sprintf("Daily at %s", at)
		}
	}
	return ""
}

// describeWeekly names the "MM HH * * <dow>" shapes: the weekday preset,
// weekends, a single day, or an explicit list of days.
func describeWeekly(dow, at string) string {
	days, ok := dowDays(dow)
	if !ok {
		return ""
	}
	switch {
	case sameDays(days, []int{1, 2, 3, 4, 5}):
		return "Every weekday at " + at
	case sameDays(days, []int{0, 6}):
		return "Weekends at " + at
	case len(days) == 1:
		return "Weekly on " + dayNames[days[0]] + " at " + at
	default:
		names := make([]string, len(days))
		for i, d := range days {
			names[i] = dayNames[d]
		}
		return "Weekly on " + joinAnd(names) + " at " + at
	}
}

// customDescription is the fallback for valid expressions outside the presets.
func customDescription(expr string) string {
	return fmt.Sprintf("Custom schedule (%s)", strings.TrimSpace(expr))
}

// dowDays converts a day-of-week field of plain numbers, day names, or the
// "1-5" range into the day numbers it names (0 = Sunday, cron's 7 folded to
// 0), deduplicated in first-appearance order. Ranges other than "1-5" and
// steps are not presets and report not-ok.
func dowDays(field string) ([]int, bool) {
	tokens := strings.Split(field, ",")
	var days []int
	for _, tok := range tokens {
		if tok == "1-5" && len(tokens) == 1 {
			return []int{1, 2, 3, 4, 5}, true
		}
		if strings.ContainsAny(tok, "-*/") {
			return nil, false
		}
		d, ok := dowNumber(tok)
		if !ok {
			return nil, false
		}
		if !containsDay(days, d) {
			days = append(days, d)
		}
	}
	return days, len(days) > 0
}

// dowNumber parses one day-of-week token, a number (with 7 meaning Sunday)
// or a case-insensitive three-letter name.
func dowNumber(tok string) (int, bool) {
	if n, err := strconv.Atoi(tok); err == nil {
		if n == 7 {
			n = 0
		}
		if n < 0 || n > 6 {
			return 0, false
		}
		return n, true
	}
	for i, name := range dayNames {
		if strings.EqualFold(tok, name[:3]) {
			return i, true
		}
	}
	return 0, false
}

// plainNumber reports whether s is exactly a non-negative integer and
// returns its value; "*" and ranges or steps are not plain numbers.
func plainNumber(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, false
	}
	return n, true
}

// stepNumber returns the N of a "*/N" field.
func stepNumber(s string) (int, bool) {
	prefix, suffix, found := strings.Cut(s, "/")
	if !found || prefix != "*" {
		return 0, false
	}
	return plainNumber(suffix)
}

// everyMinutes names the minutely preset, with the N = 1 case in the singular.
func everyMinutes(n int) string {
	if n == 1 {
		return "Every minute"
	}
	return fmt.Sprintf("Every %d minutes", n)
}

// everyHours names the hourly preset at a fixed minute, with the N = 1 case
// in the singular.
func everyHours(n, minute int) string {
	if n == 1 {
		return fmt.Sprintf("Every hour at :%02d", minute)
	}
	return fmt.Sprintf("Every %d hours at :%02d", n, minute)
}

// containsDay reports whether days already holds d.
func containsDay(days []int, d int) bool {
	for _, have := range days {
		if have == d {
			return true
		}
	}
	return false
}

// sameDays compares day lists as sets.
func sameDays(days, want []int) bool {
	if len(days) != len(want) {
		return false
	}
	for _, d := range want {
		if !containsDay(days, d) {
			return false
		}
	}
	return true
}

// joinAnd joins names with ", " and a final " and ".
func joinAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " and " + parts[1]
	default:
		return strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
	}
}

// Preview is the payload the schedule editor and the scheduler tool show for
// a recurrence under composition: its description and next three fires when
// valid, the parse error when not.
type Preview struct {
	OK          bool        `json:"ok"`
	Error       string      `json:"error,omitempty"`
	Description string      `json:"description,omitempty"`
	Next        []time.Time `json:"next,omitempty"`
}

// PreviewAt validates expr and returns its description plus the next three
// fires after now, in now's location. An invalid expression yields OK=false
// carrying the parse error text.
func PreviewAt(expr string, now time.Time) Preview {
	if _, err := ParseFiveField(expr); err != nil {
		return Preview{Error: err.Error()}
	}
	next, err := NextN(expr, now, 3)
	if err != nil {
		return Preview{Error: err.Error()}
	}
	return Preview{
		OK:          true,
		Description: Describe(expr),
		Next:        next,
	}
}
