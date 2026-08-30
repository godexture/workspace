package id3

import (
	"strings"

	"github.com/godexture/godec/media/carrier"
	"github.com/godexture/godec/media/metadata"
	"github.com/godexture/godec/media/tag"
	"github.com/godexture/godec/plugin"
)

type v2LegacyDatePlan struct {
	output map[int]v2LegacyDate
	skip   map[int]bool
}

type v2LegacyDate struct {
	value  tag.PartialDate
	origin metadata.Origin
}

type v2LegacyDatePart struct {
	frame  v2Frame
	value  string
	origin metadata.Origin
}

func v2PlanLegacyDates(tagData v2Tag, frames []v2Frame, slot carrier.ID, encoding plugin.Identity, root metadata.BlockID) v2LegacyDatePlan {
	plan := v2LegacyDatePlan{output: make(map[int]v2LegacyDate), skip: make(map[int]bool)}
	if tagData.version > 3 || tagData.decodeUnsafe {
		return plan
	}
	var years, dates, times []v2LegacyDatePart
	for _, frame := range frames {
		kind, ok := v2LegacyDateKind(frame.id)
		if !ok {
			continue
		}
		data, ok := v2FrameData(tagData, frame)
		if !ok {
			continue
		}
		values, ok := v2DecodeText(tagData.version, data)
		if !ok || len(values) != 1 {
			continue
		}
		part := v2LegacyDatePart{
			frame:  frame,
			value:  values[0],
			origin: metadata.Origin{Carrier: slot, Encoding: encoding, Block: root, Native: frame.id},
		}
		switch kind {
		case "year":
			years = append(years, part)
		case "date":
			dates = append(dates, part)
		case "time":
			times = append(times, part)
		}
	}
	for index, year := range years {
		date, ok := v2LegacyYear(year.value)
		if !ok {
			continue
		}
		selected := []v2LegacyDatePart{year}
		if index < len(dates) {
			if value, ok := v2LegacyDay(date, dates[index].value); ok {
				date = value
				selected = append(selected, dates[index])
			}
		}
		if index < len(times) {
			if value, ok := v2LegacyTime(date, times[index].value); ok {
				date = value
				selected = append(selected, times[index])
			}
		}
		first := selected[0]
		for _, part := range selected[1:] {
			if part.frame.offset < first.frame.offset {
				first = part
			}
		}
		for _, part := range selected {
			plan.skip[part.frame.offset] = true
		}
		plan.output[first.frame.offset] = v2LegacyDate{value: date, origin: first.origin}
	}
	return plan
}

func v2LegacyDateKind(frameID string) (string, bool) {
	switch frameID {
	case "TYER", "TYE":
		return "year", true
	case "TDAT", "TDA":
		return "date", true
	case "TIME", "TIM":
		return "time", true
	}
	return "", false
}

func v2LegacyYear(value string) (tag.PartialDate, bool) {
	if len(value) != 4 || !v2Digits(value) {
		return tag.PartialDate{}, false
	}
	parsed, err := tag.ParseDate(value)
	return parsed, err == nil
}

func v2ParseTDRC(value string) (tag.PartialDate, bool) {
	if !v2PlainTimestamp(value) {
		return tag.PartialDate{}, false
	}
	parsed, err := tag.ParseDate(value)
	return parsed, err == nil && parsed.ToISOString() == value
}

func v2PlainTimestamp(value string) bool {
	length := len(value)
	if length != 4 && length != 7 && length != 10 && length != 13 && length != 16 && length != 19 {
		return false
	}
	for index, byteValue := range []byte(value) {
		switch index {
		case 4, 7:
			if byteValue != '-' {
				return false
			}
		case 10:
			if byteValue != 'T' {
				return false
			}
		case 13, 16:
			if byteValue != ':' {
				return false
			}
		default:
			if byteValue < '0' || byteValue > '9' {
				return false
			}
		}
	}
	return true
}

func v2LegacyDay(year tag.PartialDate, value string) (tag.PartialDate, bool) {
	if len(value) != 4 || !v2Digits(value) {
		return tag.PartialDate{}, false
	}
	iso := year.ToISOString() + "-" + value[2:] + "-" + value[:2]
	parsed, err := tag.ParseDate(iso)
	return parsed, err == nil
}

func v2LegacyTime(date tag.PartialDate, value string) (tag.PartialDate, bool) {
	if len(value) != 4 || !v2Digits(value) || !strings.Contains(date.ToISOString(), "-") {
		return tag.PartialDate{}, false
	}
	parsed, err := tag.ParseDate(date.ToISOString() + "T" + value[:2] + ":" + value[2:])
	return parsed, err == nil
}

func v2Digits(value string) bool {
	for _, rune := range value {
		if rune < '0' || rune > '9' {
			return false
		}
	}
	return true
}
