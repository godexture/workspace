package date

import (
	"fmt"
	"time"

	"github.com/godexture/godec/sdk/optional"
)

type Partial struct {
	year  optional.Optional[int32]
	month optional.Optional[int8]
	day   optional.Optional[int8]

	hour   optional.Optional[int8]
	minute optional.Optional[int8]
	second optional.Optional[int8]
}

func (d Partial) HasValue() bool {
	return d.year.Exists() || d.month.Exists() || d.day.Exists() || d.hour.Exists() || d.minute.Exists() || d.second.Exists()
}

func (d Partial) Year() optional.Optional[int32]  { return d.year }
func (d Partial) Month() optional.Optional[int8]  { return d.month }
func (d Partial) Day() optional.Optional[int8]    { return d.day }
func (d Partial) Hour() optional.Optional[int8]   { return d.hour }
func (d Partial) Minute() optional.Optional[int8] { return d.minute }
func (d Partial) Second() optional.Optional[int8] { return d.second }

type parseLayout struct {
	layout    string
	hasYear   bool
	hasMonth  bool
	hasDay    bool
	hasHour   bool
	hasMinute bool
	hasSecond bool
}

var layouts = []parseLayout{
	// ISO 8601 / RFC 3339 (及びその精度落ちバージョン)
	{time.RFC3339, true, true, true, true, true, true},
	{"2006-01-02T15:04:05", true, true, true, true, true, true},
	{"2006-01-02T15:04", true, true, true, true, true, false},
	{"2006-01-02T15", true, true, true, true, false, false},
	{"2006-01-02", true, true, true, false, false, false},
	{"2006-01", true, true, false, false, false, false},
	{"2006", true, false, false, false, false, false},

	// スラッシュ区切り (及びその精度落ちバージョン)
	{"2006/01/02 15:04:05", true, true, true, true, true, true},
	{"2006/01/02 15:04", true, true, true, true, true, false},
	{"2006/01/02 15", true, true, true, true, false, false},
	{"2006/01/02", true, true, true, false, false, false},
	{"2006/01", true, true, false, false, false, false},

	// RFC1123 (4桁年) & そのバリエーション (曜日なし、秒なし、タイムゾーンなし/ありなど)
	{time.RFC1123, true, true, true, true, true, true},                    // "Mon, 02 Jan 2006 15:04:05 MST"
	{time.RFC1123Z, true, true, true, true, true, true},                   // "Mon, 02 Jan 2006 15:04:05 -0700"
	{"Mon, 02 Jan 2006 15:04:05", true, true, true, true, true, true},     // 曜日あり、秒あり、タイムゾーンなし
	{"Mon, 02 Jan 2006 15:04 MST", true, true, true, true, true, false},   // 曜日あり、秒なし、タイムゾーンあり
	{"Mon, 02 Jan 2006 15:04 -0700", true, true, true, true, true, false}, // 曜日あり、秒なし、タイムゾーン数値
	{"Mon, 02 Jan 2006 15:04", true, true, true, true, true, false},       // 曜日あり、秒なし、タイムゾーンなし
	{"02 Jan 2006 15:04:05 MST", true, true, true, true, true, true},      // 曜日なし
	{"02 Jan 2006 15:04:05 -0700", true, true, true, true, true, true},    // 曜日なし
	{"02 Jan 2006 15:04 MST", true, true, true, true, true, false},        // 曜日なし、秒なし
	{"02 Jan 2006 15:04 -0700", true, true, true, true, true, false},      // 曜日なし、秒なし
	{"02 Jan 2006", true, true, true, false, false, false},                // 日付のみ (曜日・時間なし)

	// RFC822 (2桁年) & そのバリエーション (秒あり/なし、タイムゾーンなし/あり)
	{time.RFC822, true, true, true, true, true, false},               // "02 Jan 06 15:04 MST"
	{time.RFC822Z, true, true, true, true, true, false},              // "02 Jan 06 15:04 -0700"
	{"02 Jan 06 15:04:05 MST", true, true, true, true, true, true},   // 秒あり
	{"02 Jan 06 15:04:05 -0700", true, true, true, true, true, true}, // 秒あり
	{"02 Jan 06", true, true, true, false, false, false},             // 日付のみ
	{"Jan 06", true, true, false, false, false, false},               // 年月のみ

	// 英語表記 (その他)
	{"January 2, 2006", true, true, true, false, false, false},
	{"Jan 2, 2006", true, true, true, false, false, false},
	{"January 2006", true, true, false, false, false, false},
	{"Jan 2006", true, true, false, false, false, false},
}

func NewPartial(str string) (Partial, error) {
	var d Partial

	if str == "" {
		return d, fmt.Errorf("empty string")
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout.layout, str); err == nil {
			if layout.hasYear {
				d.year = optional.Some(int32(t.Year()))
			}
			if layout.hasMonth {
				d.month = optional.Some(int8(t.Month()))
			}
			if layout.hasDay {
				d.day = optional.Some(int8(t.Day()))
			}
			if layout.hasHour {
				d.hour = optional.Some(int8(t.Hour()))
			}
			if layout.hasMinute {
				d.minute = optional.Some(int8(t.Minute()))
			}
			if layout.hasSecond {
				d.second = optional.Some(int8(t.Second()))
			}
			return d, nil
		}
	}

	return d, fmt.Errorf("failed to parse date from string %q: unrecognized format", str)
}

func (d Partial) ToISOString() string {
	if !d.year.Exists() {
		return ""
	}

	str := fmt.Sprintf("%04d", d.year.ValueOr(0))
	if d.month.Exists() {
		str += fmt.Sprintf("-%02d", d.month.ValueOr(0))
		if d.day.Exists() {
			str += fmt.Sprintf("-%02d", d.day.ValueOr(0))
		}
	}
	if d.hour.Exists() {
		str += fmt.Sprintf("T%02d", d.hour.ValueOr(0))
		if d.minute.Exists() {
			str += fmt.Sprintf(":%02d", d.minute.ValueOr(0))
			if d.second.Exists() {
				str += fmt.Sprintf(":%02d", d.second.ValueOr(0))
			}
		}
	}
	return str
}
