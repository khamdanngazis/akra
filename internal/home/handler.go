// Package home menyediakan halaman beranda setelah login.
package home

import (
	"net/http"

	"github.com/alexedwards/scs/v2"

	"akra/internal/auth"
)

// Renderer merender halaman dari web/templates.
type Renderer interface {
	Render(w http.ResponseWriter, r *http.Request, status int, name string, data any)
}

type page struct {
	User  auth.Identity
	Flash string
	Error string
}

// Handler mengembalikan handler halaman beranda. Harus dipasang di dalam
// grup RequireLogin.
//
// Peran: semua peran. Menu per peran dikerjakan di T51.
func Handler(rd Renderer, sessions *scs.SessionManager) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.CurrentUser(r.Context())
		rd.Render(w, r, http.StatusOK, "home", page{User: id, Flash: auth.PopFlash(r.Context(), sessions)})
	}
}
