package agent

import (
	"context"
	"errors"
	"testing"
)

func TestPlannedProgressMilestones(t *testing.T) {
	t.Parallel()

	got := plannedProgressMilestones(12)
	if len(got) != 2 || got[0] != 4 || got[1] != 8 {
		t.Fatalf("unexpected milestones: %#v", got)
	}
}

func TestShouldPublishPlannedTaskProgress(t *testing.T) {
	t.Parallel()

	milestones := plannedProgressMilestones(12)
	notified := map[int]struct{}{}
	if shouldPublishPlannedTaskProgress(context.Background(), 12, 1, plannedTaskResult{}, milestones, notified) {
		t.Fatalf("did not expect early success notification")
	}
	if !shouldPublishPlannedTaskProgress(context.Background(), 12, 4, plannedTaskResult{}, milestones, notified) {
		t.Fatalf("expected milestone notification")
	}
	notified[4] = struct{}{}
	if shouldPublishPlannedTaskProgress(context.Background(), 12, 4, plannedTaskResult{}, milestones, notified) {
		t.Fatalf("did not expect duplicate milestone notification")
	}
	if !shouldPublishPlannedTaskProgress(context.Background(), 12, 5, plannedTaskResult{ErrText: "boom"}, milestones, notified) {
		t.Fatalf("expected failure notification")
	}
	if shouldPublishPlannedTaskProgress(context.Background(), 3, 3, plannedTaskResult{}, plannedProgressMilestones(3), map[int]struct{}{}) {
		t.Fatalf("did not expect final success notification")
	}
	if shouldPublishPlannedTaskProgress(context.Background(), 12, 5, plannedTaskResult{Err: context.Canceled, ErrText: context.Canceled.Error()}, milestones, notified) {
		t.Fatalf("did not expect cancellation notification")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if shouldPublishPlannedTaskProgress(ctx, 12, 5, plannedTaskResult{Err: errors.New("worker exited after parent stop"), ErrText: "worker exited after parent stop"}, milestones, notified) {
		t.Fatalf("did not expect notification after parent cancellation")
	}
}

func TestIsPlannedTaskCancellation(t *testing.T) {
	t.Parallel()

	if !isPlannedTaskCancellation(context.Background(), plannedTaskResult{Err: context.Canceled, ErrText: context.Canceled.Error()}) {
		t.Fatalf("expected direct context cancellation to be detected")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !isPlannedTaskCancellation(ctx, plannedTaskResult{Err: errors.New("worker exited after parent stop"), ErrText: "worker exited after parent stop"}) {
		t.Fatalf("expected canceled parent context to suppress planned task result")
	}
	if isPlannedTaskCancellation(context.Background(), plannedTaskResult{Err: errors.New("boom"), ErrText: "boom"}) {
		t.Fatalf("did not expect non-cancellation error to be suppressed")
	}
}
