package queue

import "context"

type UnavailableQueue struct {
	err error
}

func NewUnavailableQueue(err error) *UnavailableQueue {
	return &UnavailableQueue{err: err}
}

func (queue *UnavailableQueue) Enqueue(context.Context, Envelope) (EnqueueResult, error) {
	return EnqueueResult{}, queue.err
}

func (queue *UnavailableQueue) Sync(context.Context, string, string, Cursor, int) ([]Pending, error) {
	return nil, queue.err
}

func (queue *UnavailableQueue) Acknowledge(context.Context, string, string, string, string) error {
	return queue.err
}

func (queue *UnavailableQueue) Ping(context.Context) error {
	return queue.err
}
