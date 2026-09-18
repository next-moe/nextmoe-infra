package main

import (
	"context"
	"fmt"
	"io"

	"api/internal/platform/catalog/service"
)

func runRelease(ctx context.Context, w io.Writer, queues *service.AdminQueueService, actor int64, note string, run bool) error {
	quarantined, held, released, err := queues.ReleaseUnheldQuarantine(ctx, actor, note, run)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "[release] quarantined=%d held=%d released=%d\n", quarantined, held, released)
	return nil
}
