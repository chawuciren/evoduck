package queue

import (
	"sync"
)

type Command struct {
	SessionKey string
	Payload    string
	Result     chan string
}

type Lane struct {
	queue chan *Command
}

func NewLane(bufferSize int) *Lane {
	return &Lane{
		queue: make(chan *Command, bufferSize),
	}
}

func (l *Lane) Enqueue(cmd *Command) {
	l.queue <- cmd
}

func (l *Lane) Dequeue() *Command {
	return <-l.queue
}

type CommandQueue struct {
	mu    sync.RWMutex
	lanes map[string]*Lane
}

func NewCommandQueue() *CommandQueue {
	return &CommandQueue{
		lanes: make(map[string]*Lane),
	}
}

func (q *CommandQueue) GetLane(sessionKey string) *Lane {
	q.mu.RLock()
	lane, ok := q.lanes[sessionKey]
	q.mu.RUnlock()
	if ok {
		return lane
	}

	q.mu.Lock()
	defer q.mu.Unlock()
	if lane, ok = q.lanes[sessionKey]; ok {
		return lane
	}

	lane = NewLane(10)
	q.lanes[sessionKey] = lane
	return lane
}
