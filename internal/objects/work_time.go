package objects

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// WorkTimeWindow describes a recurring window inside a week. Days uses ISO
// weekdays (1 = Monday ... 7 = Sunday) and Start/End use "HH:MM". When End is
// not after Start the window crosses midnight and belongs to the start day, so
// {"days": [5], "start": "22:00", "end": "06:00"} covers Friday 22:00 until
// Saturday 06:00.
type WorkTimeWindow struct {
	Days  []int  `json:"days"`
	Start string `json:"start"`
	End   string `json:"end"`
}

// WorkTimeSchedule is a weekly availability schedule. An empty schedule means
// the subject is always available: it is the "no restriction" value.
type WorkTimeSchedule struct {
	// Timezone is an IANA location name such as "Asia/Shanghai". An empty value
	// follows the system timezone from General Settings at evaluation time.
	// Runtime evaluation falls back to UTC when no system location is available.
	Timezone string `json:"timezone,omitempty"`

	Windows []WorkTimeWindow `json:"windows"`
}

const (
	defaultWorkTimeTimezone = "UTC"
	maxWorkTimeWindows      = 32
)

var workTimeLocationCache sync.Map

// IsConfigured reports whether the schedule restricts availability at all.
func (s *WorkTimeSchedule) IsConfigured() bool {
	return s != nil && len(s.Windows) > 0
}

// Location resolves the schedule timezone. Callers should validate the
// schedule first; an invalid location falls back to UTC when unvalidated data
// reaches this method.
func (s *WorkTimeSchedule) Location() *time.Location {
	tz := ""
	if s != nil {
		tz = strings.TrimSpace(s.Timezone)
	}
	if tz == "" {
		tz = defaultWorkTimeTimezone
	}

	return cachedWorkTimeLocation(tz)
}

func cachedWorkTimeLocation(tz string) *time.Location {
	if cached, ok := workTimeLocationCache.Load(tz); ok {
		return cached.(*time.Location)
	}

	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = time.UTC
	}
	actual, _ := workTimeLocationCache.LoadOrStore(tz, loc)

	return actual.(*time.Location)
}

// Normalize canonicalizes weekdays and trims the timezone. It does not change
// window order, which preserves the order the operator entered.
func (s *WorkTimeSchedule) Normalize() {
	if s == nil {
		return
	}

	s.Timezone = strings.TrimSpace(s.Timezone)
	for i := range s.Windows {
		days := slices.Clone(s.Windows[i].Days)
		slices.Sort(days)
		s.Windows[i].Days = slices.Compact(days)
		s.Windows[i].Start = strings.TrimSpace(s.Windows[i].Start)
		s.Windows[i].End = strings.TrimSpace(s.Windows[i].End)
	}
}

// Validate checks the timezone, weekdays and clock values, and normalizes the
// schedule in place.
func (s *WorkTimeSchedule) Validate() error {
	if s == nil {
		return nil
	}

	s.Normalize()
	if len(s.Windows) == 0 {
		return nil
	}
	if len(s.Windows) > maxWorkTimeWindows {
		return fmt.Errorf("work time supports at most %d windows", maxWorkTimeWindows)
	}

	if tz := s.Timezone; tz != "" {
		if _, err := time.LoadLocation(tz); err != nil {
			return fmt.Errorf("invalid work time timezone %q: %w", tz, err)
		}
	}

	for i, window := range s.Windows {
		if err := window.Validate(); err != nil {
			return fmt.Errorf("invalid work time window %d: %w", i+1, err)
		}
	}

	return nil
}

// Validate checks a single window.
func (w WorkTimeWindow) Validate() error {
	if len(w.Days) == 0 {
		return fmt.Errorf("select at least one weekday")
	}

	for _, day := range w.Days {
		if day < 1 || day > 7 {
			return fmt.Errorf("weekday %d is out of range (1-7)", day)
		}
	}

	start, err := parseClock(w.Start)
	if err != nil {
		return fmt.Errorf("invalid start time: %w", err)
	}
	end, err := parseClock(w.End)
	if err != nil {
		return fmt.Errorf("invalid end time: %w", err)
	}
	if start == end {
		return fmt.Errorf("start and end time must differ")
	}

	return nil
}

// IsActive reports whether now falls inside any configured window. An empty
// schedule is always active.
func (s *WorkTimeSchedule) IsActive(now time.Time) bool {
	return s.IsActiveIn(now, nil)
}

// IsActiveIn is IsActive with an explicit fallback location for schedules that
// do not carry their own timezone. When defaultLoc is nil such schedules use
// UTC, which keeps unvalidated legacy values predictable.
func (s *WorkTimeSchedule) IsActiveIn(now time.Time, defaultLoc *time.Location) bool {
	if !s.IsConfigured() {
		return true
	}

	loc := s.locationOrDefault(defaultLoc)
	local := now.In(loc)

	for _, window := range s.Windows {
		startMinutes, endMinutes, ok := window.minutes()
		if !ok {
			continue
		}

		duration := endMinutes - startMinutes
		if duration <= 0 {
			duration += 24 * 60
		}

		// A window can start the previous day when it crosses midnight, so check
		// today's occurrence and yesterday's occurrence.
		for offset := 0; offset <= 1; offset++ {
			day := local.AddDate(0, 0, -offset)
			if !slices.Contains(window.Days, isoWeekday(day)) {
				continue
			}

			start := time.Date(day.Year(), day.Month(), day.Day(), startMinutes/60, startMinutes%60, 0, 0, loc)
			end := start.Add(time.Duration(duration) * time.Minute)
			if !now.Before(start) && now.Before(end) {
				return true
			}
		}
	}

	return false
}

// NextStart returns the first window start strictly after now. It returns false
// when the schedule is empty.
func (s *WorkTimeSchedule) NextStart(now time.Time) (time.Time, bool) {
	return s.NextStartIn(now, nil)
}

// NextStartIn is NextStart with an explicit fallback location.
func (s *WorkTimeSchedule) NextStartIn(now time.Time, defaultLoc *time.Location) (time.Time, bool) {
	if !s.IsConfigured() {
		return time.Time{}, false
	}

	loc := s.locationOrDefault(defaultLoc)
	local := now.In(loc)

	var next time.Time
	for offset := 0; offset <= 7; offset++ {
		day := local.AddDate(0, 0, offset)
		weekday := isoWeekday(day)

		for _, window := range s.Windows {
			startMinutes, _, ok := window.minutes()
			if !ok || !slices.Contains(window.Days, weekday) {
				continue
			}

			start := time.Date(day.Year(), day.Month(), day.Day(), startMinutes/60, startMinutes%60, 0, 0, loc)
			if !start.After(now) {
				continue
			}
			if next.IsZero() || start.Before(next) {
				next = start
			}
		}

		if !next.IsZero() {
			return next, true
		}
	}

	return time.Time{}, false
}

func (s *WorkTimeSchedule) locationOrDefault(defaultLoc *time.Location) *time.Location {
	if s != nil && strings.TrimSpace(s.Timezone) != "" {
		return s.Location()
	}
	if defaultLoc != nil {
		return defaultLoc
	}

	return time.UTC
}

func (w WorkTimeWindow) minutes() (startMinutes int, endMinutes int, ok bool) {
	start, err := parseClock(w.Start)
	if err != nil {
		return 0, 0, false
	}
	end, err := parseClock(w.End)
	if err != nil {
		return 0, 0, false
	}

	return start, end, true
}

var clockPattern = regexp.MustCompile(`^([01]\d|2[0-3]):[0-5]\d$`)

func parseClock(value string) (int, error) {
	value = strings.TrimSpace(value)
	if !clockPattern.MatchString(value) {
		return 0, fmt.Errorf("expected HH:MM, got %q", value)
	}

	parsed, err := time.Parse("15:04", value)
	if err != nil {
		return 0, fmt.Errorf("expected HH:MM, got %q", value)
	}

	return parsed.Hour()*60 + parsed.Minute(), nil
}

func isoWeekday(t time.Time) int {
	weekday := int(t.Weekday())
	if weekday == 0 {
		return 7
	}

	return weekday
}
