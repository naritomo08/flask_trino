package main

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

func strconvAtoi(value string) (int, error) {
	return strconv.Atoi(value)
}

func timeBound(value, direction string, now time.Time) string {
	targetDate := now.In(jst).Format("2006-01-02")
	if value == "" {
		if direction == "from" {
			return targetDate + " 00:00:00"
		}
		return targetDate + " 23:59:59"
	}

	normalized := strings.TrimSpace(value)
	if strings.Contains(normalized, "T") {
		if parsed, err := parseISOTime(normalized); err == nil {
			if parsed.Location() != time.Local {
				parsed = parsed.In(jst)
			}
			return parsed.Format("2006-01-02 15:04:05")
		}
	}
	if parsed, err := time.Parse("15:04:05", addSeconds(normalized)); err == nil {
		return targetDate + " " + parsed.Format("15:04:05")
	}
	if direction == "from" {
		return targetDate + " 00:00:00"
	}
	return targetDate + " 23:59:59"
}

func addSeconds(value string) string {
	if len(strings.Split(value, ":")) == 2 {
		return value + ":00"
	}
	return value
}

func parseISOTime(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	for _, layout := range []string{"2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if parsed, err := time.ParseInLocation(layout, value, jst); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid time: %s", value)
}

func formatTimestamp(value any) string {
	if value == nil {
		return ""
	}
	switch v := value.(type) {
	case int64:
		return formatMillis(v)
	case int:
		return formatMillis(int64(v))
	case float64:
		if math.Trunc(v) == v {
			return formatMillis(int64(v))
		}
	case json.Number:
		if millis, err := v.Int64(); err == nil {
			return formatMillis(millis)
		}
	case time.Time:
		if v.Location() == time.Local {
			return v.Format("2006/01/02 15:04:05 JST")
		}
		return v.In(jst).Format("2006/01/02 15:04:05 JST")
	case string:
		if formatted, ok := formatTimestampString(v); ok {
			return formatted
		}
		return v
	}
	return fmt.Sprint(value)
}

func formatMillis(value int64) string {
	return time.UnixMilli(value).UTC().In(jst).Format("2006/01/02 15:04:05 JST")
}

func formatTimestampString(value string) (string, bool) {
	normalized := strings.ReplaceAll(strings.TrimSpace(value), " UTC", "Z")
	normalized = strings.ReplaceAll(normalized, " ", "T")
	normalized = strings.ReplaceAll(normalized, "Z", "+00:00")

	layouts := []string{
		time.RFC3339Nano,
		"2006-01-02T15:04:05.999999999-07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, normalized); err == nil {
			if strings.Contains(layout, "-07:00") || strings.Contains(normalized, "+") {
				return parsed.In(jst).Format("2006/01/02 15:04:05 JST"), true
			}
			return parsed.Format("2006/01/02 15:04:05 JST"), true
		}
	}
	return "", false
}
