package events

import "sync"

type Broker struct {
	mu   sync.Mutex
	next int
	subs map[int]chan string
}

func NewBroker() *Broker { return &Broker{subs: make(map[int]chan string)} }

func (b *Broker) Subscribe() (int, <-chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.next++
	ch := make(chan string, 32)
	b.subs[b.next] = ch
	return b.next, ch
}

func (b *Broker) Unsubscribe(id int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if ch, ok := b.subs[id]; ok {
		delete(b.subs, id)
		close(ch)
	}
}

func (b *Broker) Publish(jobID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, ch := range b.subs {
		select {
		case ch <- jobID:
		default:
		}
	}
}
