package tools

import "context"

type SubagentExecutionStats struct {
	Iterations  int
	Attempts    int
	Restarts    int
	FailureCode string
}

type subagentExecutionStatsKey struct{}
type subagentIterationBudgetKey struct{}

func WithSubagentExecutionStats(ctx context.Context, stats *SubagentExecutionStats) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, subagentExecutionStatsKey{}, stats)
}

func RecordSubagentExecutionStats(ctx context.Context, delta SubagentExecutionStats) {
	if ctx == nil {
		return
	}
	stats, _ := ctx.Value(subagentExecutionStatsKey{}).(*SubagentExecutionStats)
	if stats == nil {
		return
	}
	stats.Iterations += delta.Iterations
	stats.Attempts += delta.Attempts
	stats.Restarts += delta.Restarts
	if delta.FailureCode != "" {
		stats.FailureCode = delta.FailureCode
	}
}

func WithSubagentIterationBudget(ctx context.Context, budget int) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, subagentIterationBudgetKey{}, budget)
}

func SubagentIterationBudget(ctx context.Context) (int, bool) {
	if ctx == nil {
		return 0, false
	}
	budget, ok := ctx.Value(subagentIterationBudgetKey{}).(int)
	return budget, ok && budget > 0
}
