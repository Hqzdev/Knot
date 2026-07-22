package cleanup

import (
	"context"
	"errors"
	"time"

	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/metadata"
	"github.com/yaroslavfairfieldd/knot/services/attachments/internal/objectstore"
)

type Worker struct {
	metadata metadata.Store
	objects  objectstore.Store
	interval time.Duration
	batch    int
	now      func() time.Time
	report   func(error)
}

func NewWorker(metadataStore metadata.Store, objectStore objectstore.Store, interval time.Duration, batch int, report func(error)) *Worker {
	if report == nil {
		report = func(error) {}
	}
	return &Worker{metadata: metadataStore, objects: objectStore, interval: interval, batch: batch, now: time.Now, report: report}
}

func (worker *Worker) Run(ctx context.Context) {
	worker.runSweep(ctx)
	ticker := time.NewTicker(worker.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			worker.runSweep(ctx)
		}
	}
}

func (worker *Worker) Sweep(ctx context.Context) error {
	attachments, err := worker.metadata.ClaimExpired(ctx, worker.now().UTC(), worker.batch)
	if err != nil {
		return err
	}
	var failures []error
	for _, attachment := range attachments {
		if err := worker.objects.Delete(ctx, attachment.ObjectKey); err != nil {
			failures = append(failures, err)
			continue
		}
		if err := worker.metadata.Purge(ctx, attachment.ID); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(failures...)
}

func (worker *Worker) runSweep(ctx context.Context) {
	if err := worker.Sweep(ctx); err != nil && ctx.Err() == nil {
		worker.report(err)
	}
}
