package cron

import (
	"fmt"
	"strings"
	"time"
)

// SimpleCronParser 实现了一个极简的 Cron 表达式解析器 (minute hour day month dayOfWeek)
// 借鉴 goclaw 可能使用的标准 cron 逻辑，补全 ClawGo 缺失的 Expr 调度能力
type SimpleCronParser struct {
	expr string
}

func NewSimpleCronParser(expr string) *SimpleCronParser {
	return &SimpleCronParser{expr: expr}
}

func (p *SimpleCronParser) Next(from time.Time) time.Time {
	fields := strings.Fields(p.expr)
	if len(fields) != 5 {
		return time.Time{} // 格式错误
	}

	// 这是一个极简实现，仅支持 * 和 数字
	// 生产环境下建议引入 github.com/robfig/cron/v3
	next := from.Add(1 * time.Minute).Truncate(time.Minute)
	
	// 这里逻辑简化：如果不是 * 且不匹配，则递增直到匹配 (最大搜索 1 年)
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
