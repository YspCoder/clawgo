package agent

import (
	"context"
	"testing"
	"time"
)

func TestSessionSchedulerConflictSerializes(t *testing.T) {
	s := NewSessionScheduler(4)
	release1, err := s.Acquire(context.Background(), "sess-a", []string{"file:a.go"})
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}
	defer release1()

	acquired2 := make(chan struct{}, 1)
	done2 := make(chan struct{}, 1)
	go func() {
		defer func() { done2 <- struct{}{} }()
		release2, err := s.Acquire(context.Background(), "sess-a", []string{"file:a.go"})
		if err != nil {
			return
		}
		acquired2 <- struct{}{}
		release2()
	}()

	select {
	case <-acquired2:
		t.Fatalf("second conflicting run should wait")
	case <-time.After(80 * time.Millisecond):
	}

	release1()

	select {
	case <-acquired2:
	case <-time.After(time.Second):
		t.Fatalf("second run should acquire after release")
	}
	<-done2
}

func TestSessionSchedulerNonConflictingCanRunInParallel(t *testing.T) {
	s := NewSessionScheduler(4)
	release1, err := s.Acquire(context.Background(), "sess-a", []string{"file:a.go"})
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}
	defer release1()

	acquired2 := make(chan struct{}, 1)
	done2 := make(chan struct{}, 1)
	go func() {
		defer func() { done2 <- struct{}{} }()
		release2, err := s.Acquire(context.Background(), "sess-a", []string{"file:b.go"})
		if err != nil {
			return
		}
		acquired2 <- struct{}{}
		release2()
	}()

	select {
	case <-acquired2:
	case <-time.After(time.Second):
		t.Fatalf("second non-conflicting run should acquire immediately")
	}
	<-done2
}

func TestSessionSchedulerHonorsSessionMaxParallel(t *testing.T) {
	s := NewSessionScheduler(1)
	release1, err := s.Acquire(context.Background(), "sess-a", []string{"file:a.go"})
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}
	defer release1()

	acquired2 := make(chan struct{}, 1)
	go func() {
		release2, err := s.Acquire(context.Background(), "sess-a", []string{"file:b.go"})
		if err != nil {
			return
		}
		acquired2 <- struct{}{}
		release2()
	}()

	select {
	case <-acquired2:
		t.Fatalf("second run should wait when max parallel is 1")
	case <-time.After(80 * time.Millisecond):
	}

	release1()
	select {
	case <-acquired2:
	case <-time.After(time.Second):
		t.Fatalf("second run should continue after first release")
	}
}
