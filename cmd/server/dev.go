package main

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"akra/internal/platform/jobs"
)

// devRoutes mengembalikan route khusus development, atau nil di luar
// development. Route ini tanpa sesi dan tanpa CSRF, jadi tidak pernah
// dipasang di staging atau production.
func devRoutes(d appDeps) func(chi.Router) {
	if !d.dev || d.jobs == nil {
		return nil
	}
	client := d.jobs
	return func(r chi.Router) {
		// POST /dev/jobs/ping?message=... memasukkan PingJob.
		r.Post("/jobs/ping", func(w http.ResponseWriter, r *http.Request) {
			res, err := client.Insert(r.Context(), jobs.PingArgs{Message: r.URL.Query().Get("message")}, nil)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			writeJSON(w, http.StatusCreated, map[string]any{"id": res.Job.ID, "state": res.Job.State})
		})
		// GET /dev/jobs/{id} menampilkan status job.
		r.Get("/jobs/{id}", func(w http.ResponseWriter, r *http.Request) {
			id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
			if err != nil {
				http.Error(w, "invalid id", http.StatusBadRequest)
				return
			}
			job, err := client.JobGet(r.Context(), id)
			if err != nil {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id": job.ID, "kind": job.Kind, "state": job.State, "attempt": job.Attempt,
				"errors": job.Errors, "finalized_at": job.FinalizedAt,
			})
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
