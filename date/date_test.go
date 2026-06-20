package date

import (
	"testing"

	"github.com/godexture/sdk/optional"
)

func TestNewPartial(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantErr    bool
		wantYear   int32
		wantMonth  int8
		wantDay    int8
		wantHour   int8
		wantMinute int8
		wantSecond int8
	}{
		// === ISO 8601 / RFC 3339 ===
		{
			name:      "ISO year only",
			input:     "2023",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 0, wantDay: 0, wantHour: 0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:      "ISO year and month",
			input:     "2023-12",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12, wantDay: 0, wantHour: 0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:      "ISO full date",
			input:     "2023-12-01",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   1,
			wantHour:  0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:       "ISO date with hour",
			input:      "2023-12-01T15",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 0, wantSecond: 0,
		},
		{
			name:       "ISO date with hour and minute",
			input:      "2023-12-01T15:30",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 0,
		},
		{
			name:       "ISO full datetime",
			input:      "2023-12-01T15:30:45",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 45,
		},
		{
			name:       "RFC3339 with timezone",
			input:      "2023-12-01T15:30:45Z",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 45,
		},

		// === Slash separated ===
		{
			name:      "Slash year and month",
			input:     "2023/12",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12, wantDay: 0, wantHour: 0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:      "Slash full date",
			input:     "2023/12/01",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   1,
			wantHour:  0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:       "Slash date with hour",
			input:      "2023/12/01 15",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 0, wantSecond: 0,
		},
		{
			name:       "Slash date with time (no seconds)",
			input:      "2023/12/01 15:30",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 0,
		},
		{
			name:       "Slash full datetime",
			input:      "2023/12/01 15:30:45",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 45,
		},

		// === RFC1123 (4-digit year) & Variations ===
		{
			name:       "RFC1123 standard",
			input:      "Fri, 01 Dec 2023 15:30:45 MST",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 45,
		},
		{
			name:       "RFC1123Z standard",
			input:      "Fri, 01 Dec 2023 15:30:45 +0900",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 45,
		},
		{
			name:       "RFC1123 no seconds",
			input:      "Fri, 01 Dec 2023 15:30 MST",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 0,
		},
		{
			name:       "RFC1123 no weekday",
			input:      "01 Dec 2023 15:30:45 MST",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 45,
		},
		{
			name:       "RFC1123 no weekday no seconds",
			input:      "01 Dec 2023 15:30 MST",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 0,
		},
		{
			name:      "RFC1123 date only",
			input:     "01 Dec 2023",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   1,
			wantHour:  0, wantMinute: 0, wantSecond: 0,
		},

		// === RFC822 (2-digit year) & Variations ===
		{
			name:       "RFC822 standard (no seconds)",
			input:      "01 Dec 23 15:30 MST",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 0,
		},
		{
			name:       "RFC822Z standard (no seconds)",
			input:      "01 Dec 23 15:30 +0900",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 0,
		},
		{
			name:       "RFC822 with seconds",
			input:      "01 Dec 23 15:30:45 MST",
			wantErr:    false,
			wantYear:   2023,
			wantMonth:  12,
			wantDay:    1,
			wantHour:   15,
			wantMinute: 30,
			wantSecond: 45,
		},
		{
			name:      "RFC822 date only",
			input:     "01 Dec 23",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   1,
			wantHour:  0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:      "RFC822 year and month only",
			input:     "Dec 23",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   0, wantHour: 0, wantMinute: 0, wantSecond: 0,
		},

		// === English Written Format ===
		{
			name:      "English full date (Long)",
			input:     "December 1, 2023",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   1,
			wantHour:  0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:      "English full date (Short)",
			input:     "Dec 1, 2023",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   1,
			wantHour:  0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:      "English year and month (Long)",
			input:     "December 2023",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   0, wantHour: 0, wantMinute: 0, wantSecond: 0,
		},
		{
			name:      "English year and month (Short)",
			input:     "Dec 2023",
			wantErr:   false,
			wantYear:  2023,
			wantMonth: 12,
			wantDay:   0, wantHour: 0, wantMinute: 0, wantSecond: 0,
		},

		// === Invalid cases ===
		{name: "empty", input: "", wantErr: true},
		{name: "invalid text", input: "not-a-date", wantErr: true},
		{name: "broken ISO", input: "2023-12-", wantErr: true},
		{name: "double separator ISO", input: "2023--12", wantErr: true},
		{name: "too long slash", input: "2023/12/01/02", wantErr: true},
		{name: "time only missing T", input: "15:30:00", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewPartial(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewPartial(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}

			if got.Year().ValueOr(0) != tt.wantYear {
				t.Errorf("Year = %v, want %v", got.Year().ValueOr(0), tt.wantYear)
			}
			if got.Month().ValueOr(0) != tt.wantMonth {
				t.Errorf("Month = %v, want %v", got.Month().ValueOr(0), tt.wantMonth)
			}
			if got.Day().ValueOr(0) != tt.wantDay {
				t.Errorf("Day = %v, want %v", got.Day().ValueOr(0), tt.wantDay)
			}
			if got.Hour().ValueOr(0) != tt.wantHour {
				t.Errorf("Hour = %v, want %v", got.Hour().ValueOr(0), tt.wantHour)
			}
			if got.Minute().ValueOr(0) != tt.wantMinute {
				t.Errorf("Minute = %v, want %v", got.Minute().ValueOr(0), tt.wantMinute)
			}
			if got.Second().ValueOr(0) != tt.wantSecond {
				t.Errorf("Second = %v, want %v", got.Second().ValueOr(0), tt.wantSecond)
			}
		})
	}
}

func TestPartial_ToISOString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"year only", "2023", "2023"},
		{"year and month", "2023-12", "2023-12"},
		{"full date", "2023-12-01", "2023-12-01"},
		{"date with hour", "2023-12-01T15", "2023-12-01T15"},
		{"date with hour and minute", "2023-12-01T15:30", "2023-12-01T15:30"},
		{"full datetime", "2023-12-01T15:30:45", "2023-12-01T15:30:45"},
		{"RFC1123 standard", "Fri, 01 Dec 2023 15:30:45 MST", "2023-12-01T15:30:45"},
		{"empty string", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, _ := NewPartial(tt.input)
			if got := p.ToISOString(); got != tt.expected {
				t.Errorf("Partial.ToISOString() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestPartial_ToISOString_EdgeCases(t *testing.T) {
	// Missing year should return empty string
	p1 := Partial{
		month: optional.Some[int8](12),
	}
	if got := p1.ToISOString(); got != "" {
		t.Errorf("Expected empty string when year is missing, got %q", got)
	}

	// Missing day but has hour
	p2 := Partial{
		year:  optional.Some[int32](2023),
		month: optional.Some[int8](12),
		hour:  optional.Some[int8](15),
	}
	if got := p2.ToISOString(); got != "2023-12T15" {
		t.Errorf("Expected '2023-12T15', got %q", got)
	}
}
