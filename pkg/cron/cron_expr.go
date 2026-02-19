package cron

import (
	"fmt"
	"strings"
	"time"
)

// SimpleCronParser is a minimal cron expression parser (minute hour day month dayOfWeek).
// It approximates standard cron behavior to provide missing Expr scheduling support.
type SimpleCronParser struct {
	expr string
}

func NewSimpleCronParser(expr string) *SimpleCronParser {
	return &SimpleCronParser{expr: expr}
}

func (p *SimpleCronParser) Next(from time.Time) time.Time {
	fields := strings.Fields(p.expr)
	if len(fields) != 5 {
		return time.Time{} // Invalid format
	}

	// This minimal implementation only supports "*" and exact numbers.
	// For production, use github.com/robfig/cron/v3.
	next := from.Add(1 * time.Minute).Truncate(time.Minute)

	// Simplified logic: if it is not "*" and does not match, keep incrementing until matched
	// (up to one year of search).
	for i := 0; i < 525600; i++ {
		if p.match(next, fields) {
			return next
		}
		next = next.Add(1 * time.Minute)
	}
	return time.Time{}
}

func (p *SimpleCronParser) match(t time.Time, fields []string) bool {
	return matchField(fmt.Sprintf("%d", t.Minute()), fields[0]) &&
		matchField(fmt.Sprintf("%d", t.Hour()), fields[1]) &&
		matchField(fmt.Sprintf("%d", t.Day()), fields[2]) &&
		matchField(fmt.Sprintf("%d", int(t.Month())), fields[3]) &&
		matchField(fmt.Sprintf("%d", int(t.Weekday())), fields[4])
}

func matchField(val, field string) bool {
	if field == "*" {
		return true
	}
	return val == field
}
