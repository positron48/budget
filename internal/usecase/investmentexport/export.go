// Package investmentexport defines private, expiring canonical export jobs.
package investmentexport

import (
	"context"
	"time"
)

const ChunkBytes = 256 * 1024

type Job struct {
	ID, State, SHA256, ErrorCode string
	ByteSize                     int64
	CreatedAt                    time.Time
	CompletedAt, ExpiresAt       *time.Time
}
type Chunk struct {
	Data          []byte
	Offset, Total int64
	SHA256        string
}
type Processor interface {
	ProcessExport(context.Context) (bool, error)
}

type Repository interface {
	StartExport(context.Context, string, string, string) (Job, error)
	GetExport(context.Context, string, string, string) (Job, error)
	ReadExportChunk(context.Context, string, string, string, int64) (Chunk, error)
	ProcessExport(context.Context) (bool, error)
}

// Run stops between bounded jobs; one process claims at most one export at a time.
func Run(ctx context.Context, r Processor, onError func()) {
	for ctx.Err() == nil {
		worked, err := r.ProcessExport(ctx)
		if err != nil && ctx.Err() == nil && onError != nil {
			onError()
		}
		if worked && err == nil {
			continue
		}
		delay := 2 * time.Second
		if err != nil {
			delay = time.Minute
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
