package agent

import "testing"

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
	if shouldPublishPlannedTaskProgress(12, 1, plannedTaskResult{}, milestones, notified) {
		t.Fatalf("did not expect early success notification")
	}
	if !shouldPublishPlannedTaskProgress(12, 4, plannedTaskResult{}, milestones, notified) {
		t.Fatalf("expected milestone notification")
	}
	notified[4] = struct{}{}
	if shouldPublishPlannedTaskProgress(12, 4, plannedTaskResult{}, milestones, notified) {
		t.Fatalf("did not expect duplicate milestone notification")
	}
	if !shouldPublishPlannedTaskProgress(12, 5, plannedTaskResult{ErrText: "boom"}, milestones, notified) {
		t.Fatalf("expected failure notification")
	}
	if shouldPublishPlannedTaskProgress(3, 3, plannedTaskResult{}, plannedProgressMilestones(3), map[int]struct{}{}) {
		t.Fatalf("did not expect final success notification")
	}
}
