package events

import "sync"

// TypedBus is a lightweight generic pub/sub bus for internal architecture events.
type TypedBus[T any] struct {
	mu   sync.RWMutex
	subs []chan T
}

func NewTypedBus[T any]() *TypedBus[T] {
	return &TypedBus[T]{subs: make([]chan T, 0)}
}

func (b *TypedBus[T]) Subscribe(buffer int) <-chan T {
	if buffer <= 0 {
		buffer = 8
	}
	ch := make(chan T, buffer)
	b.mu.Lock()
	b.subs = append(b.subs, ch)
	b.mu.Unlock()
	return ch
}

func (b *TypedBus[T]) Publish(v T) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	for _, ch := range b.subs {
		select {
		case ch <- v:
		default:
			// drop on backpressure to keep publisher non-blocking
		}
	}
}
