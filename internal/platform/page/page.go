// Package page berisi halaman error bersama untuk semua modul.
package page

import (
	"log/slog"
	"net/http"
)

// Renderer merender halaman dari web/templates.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, status int, name string, data any)
}

// ErrorView adalah data untuk templates/error.html. User berisi
// auth.Identity agar header aplikasi tetap tampil; nil untuk halaman publik.
type ErrorView struct {
	User    any
	Title   string
	Message string
}

// NotFound merender halaman 404 dengan pesan message.
func NotFound(rd Renderer, w http.ResponseWriter, r *http.Request, user any, message string) {
	rd.Render(w, r, http.StatusNotFound, "error", ErrorView{User: user, Title: "Data tidak ditemukan", Message: message})
}

// ServerError mencatat err lalu merender halaman 500 tanpa detail teknis.
func ServerError(rd Renderer, logger *slog.Logger, w http.ResponseWriter, r *http.Request, user any, op string, err error) {
	logger.ErrorContext(r.Context(), op, "error", err)
	rd.Render(w, r, http.StatusInternalServerError, "error", ErrorView{
		User: user, Title: "Terjadi kesalahan", Message: "Terjadi kesalahan pada server. Silakan coba lagi.",
	})
}

// Forbidden merender halaman 403 dengan pesan message.
func Forbidden(rd Renderer, w http.ResponseWriter, r *http.Request, user any, message string) {
	rd.Render(w, r, http.StatusForbidden, "error", ErrorView{User: user, Title: "Akses ditolak", Message: message})
}

// BadRequest merender halaman 400 dengan judul dan pesan.
func BadRequest(rd Renderer, w http.ResponseWriter, r *http.Request, user any, title, message string) {
	rd.Render(w, r, http.StatusBadRequest, "error", ErrorView{User: user, Title: title, Message: message})
}
