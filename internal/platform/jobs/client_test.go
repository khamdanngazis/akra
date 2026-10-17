package jobs

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
	"github.com/riverqueue/river/rivertype"

	"akra/internal/platform/database"
)

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	pool, err := database.Connect(context.Background(), dsn, 10)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// cleanupJobs menghapus job uji dengan kind tertentu dari river_job.
func cleanupJobs(t *testing.T, pool *pgxpool.Pool, kinds ...string) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM river_job WHERE kind = ANY($1)`, kinds)
	})
}

// waitFor menunggu cond bernilai true, paling lama timeout.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return cond()
}

func TestRiverMigrationsMatchLibrary(t *testing.T) {
	pool := testPool(t)
	migrator, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := migrator.Validate(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.OK {
		t.Errorf("river schema is not up to date: %v", res.Messages)
	}
}

func TestPingJobCompletes(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanupJobs(t, pool, "ping")
	workers := NewWorkers()
	river.AddWorker(workers, &PingWorker{Logger: discard})
	client, err := NewClient(pool, workers, Config{Workers: 2}, discard)
	if err != nil {
		t.Fatal(err)
	}
	completed, stop := client.Subscribe(river.EventKindJobCompleted)
	defer stop()
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer Shutdown(client, 5*time.Second, discard)

	res, err := client.Insert(ctx, PingArgs{Message: "halo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-completed:
		if ev.Job.ID != res.Job.ID {
			t.Fatalf("completed job %d, want %d", ev.Job.ID, res.Job.ID)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("ping job did not complete")
	}
	var state string
	if err := pool.QueryRow(ctx, `SELECT state FROM river_job WHERE id = $1`, res.Job.ID).Scan(&state); err != nil || state != "completed" {
		t.Errorf("river_job state = %q, %v; want completed", state, err)
	}
}

// blockArgs adalah job uji yang berjalan sampai context-nya dibatalkan.
type blockArgs struct{}

func (blockArgs) Kind() string { return "test_block" }

type blockWorker struct {
	river.WorkerDefaults[blockArgs]
	running atomic.Int32
	maxSeen atomic.Int32
}

func (w *blockWorker) Work(ctx context.Context, job *river.Job[blockArgs]) error {
	n := w.running.Add(1)
	defer w.running.Add(-1)
	for {
		cur := w.maxSeen.Load()
		if n <= cur || w.maxSeen.CompareAndSwap(cur, n) {
			break
		}
	}
	<-ctx.Done()
	return ctx.Err()
}

func TestShutdownCancelsAndRetriesRunningJob(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanupJobs(t, pool, "test_block")
	worker := &blockWorker{}
	workers := NewWorkers()
	river.AddWorker(workers, worker)
	client, err := NewClient(pool, workers, Config{Workers: 1}, discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	res, err := client.Insert(ctx, blockArgs{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !waitFor(t, 10*time.Second, func() bool { return worker.running.Load() == 1 }) {
		t.Fatal("job did not start")
	}

	Shutdown(client, 300*time.Millisecond, discard)

	insertOnly, err := NewClient(pool, nil, Config{}, discard)
	if err != nil {
		t.Fatal(err)
	}
	// Job yang dibatalkan dijadwalkan ulang: retryable, lalu available saat
	// jadwal ulangnya tiba. Yang penting: tidak tertinggal "running" dan
	// errornya tercatat.
	var job *rivertype.JobRow
	ok := waitFor(t, 5*time.Second, func() bool {
		job, err = insertOnly.JobGet(ctx, res.Job.ID)
		return err == nil && (job.State == rivertype.JobStateRetryable || job.State == rivertype.JobStateAvailable)
	})
	if !ok {
		t.Fatalf("job after shutdown = %+v (err %v); want retryable or available", job, err)
	}
	if job.Attempt != 1 || len(job.Errors) == 0 {
		t.Errorf("attempt = %d, errors = %d; want 1 attempt with a recorded error", job.Attempt, len(job.Errors))
	}
}

func TestWorkerCountFollowsConfig(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	cleanupJobs(t, pool, "test_block")
	worker := &blockWorker{}
	workers := NewWorkers()
	river.AddWorker(workers, worker)
	client, err := NewClient(pool, workers, Config{Workers: 2}, discard)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Start(ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 5; i++ {
		if _, err := client.Insert(ctx, blockArgs{}, nil); err != nil {
			t.Fatal(err)
		}
	}
	if !waitFor(t, 10*time.Second, func() bool { return worker.running.Load() == 2 }) {
		t.Fatalf("running = %d, want 2", worker.running.Load())
	}
	time.Sleep(300 * time.Millisecond) // beri kesempatan job ketiga (seharusnya tidak) mulai
	if got := worker.maxSeen.Load(); got != 2 {
		t.Errorf("max concurrent jobs = %d, want 2 (RIVER_WORKERS)", got)
	}
	Shutdown(client, 100*time.Millisecond, discard)

	if _, err := NewClient(pool, workers, Config{Workers: 0}, discard); err == nil {
		t.Error("NewClient with 0 workers: error = nil")
	}
}
