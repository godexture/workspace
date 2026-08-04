// Package tag provides the shared metadata vocabulary.
package tag

import (
	"fmt"
	"time"
)

// Date preserves the precision of a metadata date. A source that only says
// "1985" stays distinct from one that says "1985-01-01".
type Date struct {
	year, month, day     int
	hour, minute, second int
	hasYear, hasMonth    bool
	hasDay, hasHour      bool
	hasMinute, hasSecond bool
}

// ParseDate parses the date forms used by the existing metadata encodings.
// Missing components remain absent instead of being filled with zero values.
func ParseDate(value string) (Date, error) {
	if value == "" {
		return Date{}, fmt.Errorf("empty date")
	}
	for _, layout := range dateLayouts {
		parsed, err := time.Parse(layout.layout, value)
		if err != nil {
			continue
		}
		return Date{
			year:      parsed.Year(),
			month:     int(parsed.Month()),
			day:       parsed.Day(),
			hour:      parsed.Hour(),
			minute:    parsed.Minute(),
			second:    parsed.Second(),
			hasYear:   layout.year,
			hasMonth:  layout.month,
			hasDay:    layout.day,
			hasHour:   layout.hour,
			hasMinute: layout.minute,
			hasSecond: layout.second,
		}, nil
	}
	return Date{}, fmt.Errorf("failed to parse date %q", value)
}

// Parse is an alias with a conventional parser name for metadata decoders.
func Parse(value string) (Date, error) { return ParseDate(value) }

// NewDate constructs a Date by parsing an encoding value.
func NewDate(value string) (Date, error) { return ParseDate(value) }

func (d Date) HasValue() bool {
	return d.hasYear || d.hasMonth || d.hasDay || d.hasHour || d.hasMinute || d.hasSecond
}

func (d Date) Year() (int, bool)   { return d.year, d.hasYear }
func (d Date) Month() (int, bool)  { return d.month, d.hasMonth }
func (d Date) Day() (int, bool)    { return d.day, d.hasDay }
func (d Date) Hour() (int, bool)   { return d.hour, d.hasHour }
func (d Date) Minute() (int, bool) { return d.minute, d.hasMinute }
func (d Date) Second() (int, bool) { return d.second, d.hasSecond }

// ToISOString writes the most precise ISO-like representation available. It
// intentionally omits timezone information because the old metadata contract
// stores calendar components, not an instant.
func (d Date) ToISOString() string {
	if !d.hasYear {
		return ""
	}
	result := fmt.Sprintf("%04d", d.year)
	if d.hasMonth {
		result += fmt.Sprintf("-%02d", d.month)
		if d.hasDay {
			result += fmt.Sprintf("-%02d", d.day)
		}
	}
	if d.hasHour {
		result += fmt.Sprintf("T%02d", d.hour)
		if d.hasMinute {
			result += fmt.Sprintf(":%02d", d.minute)
			if d.hasSecond {
				result += fmt.Sprintf(":%02d", d.second)
			}
		}
	}
	return result
}

func (d Date) String() string { return d.ToISOString() }

type dateLayout struct {
	layout               string
	year, month, day     bool
	hour, minute, second bool
}

var dateLayouts = []dateLayout{
	{layout: time.RFC3339, year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "2006-01-02T15:04:05", year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "2006-01-02T15:04", year: true, month: true, day: true, hour: true, minute: true},
	{layout: "2006-01-02T15", year: true, month: true, day: true, hour: true},
	{layout: "2006-01-02", year: true, month: true, day: true},
	{layout: "2006-01", year: true, month: true},
	{layout: "2006", year: true},
	{layout: "2006/01/02 15:04:05", year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "2006/01/02 15:04", year: true, month: true, day: true, hour: true, minute: true},
	{layout: "2006/01/02 15", year: true, month: true, day: true, hour: true},
	{layout: "2006/01/02", year: true, month: true, day: true},
	{layout: "2006/01", year: true, month: true},
	{layout: time.RFC1123, year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: time.RFC1123Z, year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "Mon, 02 Jan 2006 15:04:05", year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "Mon, 02 Jan 2006 15:04 MST", year: true, month: true, day: true, hour: true, minute: true},
	{layout: "Mon, 02 Jan 2006 15:04 -0700", year: true, month: true, day: true, hour: true, minute: true},
	{layout: "Mon, 02 Jan 2006 15:04", year: true, month: true, day: true, hour: true, minute: true},
	{layout: "02 Jan 2006 15:04:05 MST", year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "02 Jan 2006 15:04:05 -0700", year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "02 Jan 2006 15:04 MST", year: true, month: true, day: true, hour: true, minute: true},
	{layout: "02 Jan 2006 15:04 -0700", year: true, month: true, day: true, hour: true, minute: true},
	{layout: "02 Jan 2006", year: true, month: true, day: true},
	{layout: time.RFC822, year: true, month: true, day: true, hour: true, minute: true},
	{layout: time.RFC822Z, year: true, month: true, day: true, hour: true, minute: true},
	{layout: "02 Jan 06 15:04:05 MST", year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "02 Jan 06 15:04:05 -0700", year: true, month: true, day: true, hour: true, minute: true, second: true},
	{layout: "02 Jan 06", year: true, month: true, day: true},
	{layout: "Jan 06", year: true, month: true},
	{layout: "January 2, 2006", year: true, month: true, day: true},
	{layout: "Jan 2, 2006", year: true, month: true, day: true},
	{layout: "January 2006", year: true, month: true},
	{layout: "Jan 2006", year: true, month: true},
}
