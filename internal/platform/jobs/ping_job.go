package jobs

import (
	"context"
	"log/slog"

	"github.com/riverqueue/river"
)

// PingArgs adalah job contoh untuk memeriksa antrean berjalan.
type PingArgs struct {
	Message string `json:"message"`
}

// Kind mengembalikan nama jenis job.
func (PingArgs) Kind() string { return "ping" }

// PingWorker menulis pesan ke log lalu selesai.
type PingWorker struct {
	river.WorkerDefaults[PingArgs]
	Logger *slog.Logger
}

// Work menjalankan job ping.
func (w *PingWorker) Work(ctx context.Context, job *river.Job[PingArgs]) error {
	w.Logger.InfoContext(ctx, "ping job", "job_id", job.ID, "message", job.Args.Message)
	return nil
}
