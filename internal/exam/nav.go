package exam

import (
	"context"
	"net/http"
	"slices"

	"akra/internal/auth"
	"akra/internal/platform/page"
	"akra/internal/platform/subnav"
)

// Sub-navigasi modul ujian (T157): tampil di bawah header setiap halaman
// staf /ujian, isinya sesuai hak akses pengguna (matriks T125) dan tahap
// rilis modul.

// Nav mengembalikan item sub-navigasi modul ujian untuk a. review: telaah
// soal aktif di sekolah.
func Nav(a Actor, m Module, review bool) []subnav.Item {
	staff := slices.Contains(a.Roles, roleAdmin) || slices.Contains(a.Roles, roleTeacher)
	items := []subnav.Item{{Label: "Ringkasan", Path: "/ujian", Exact: true}}
	if a.CanAny(ActBank, Read) {
		items = append(items, subnav.Item{Label: "Bank soal", Path: "/ujian/bank/",
			Also: []string{"/ujian/soal", "/ujian/tp", "/ujian/impor", "/ujian/stimulus", "/ujian/draf", "/ujian/aset", "/ujian/pratinjau"}})
	}
	if a.CanAny(ActClassExam, Read) || a.CanAny(ActAdmissionTest, Read) {
		items = append(items, subnav.Item{Label: "Ujian", Path: "/ujian/paket/", Also: []string{"/ujian/latihan"}})
	}
	if m.Has(R3) && a.CanAny(ActAgenda, Read) {
		items = append(items, subnav.Item{Label: "Agenda", Path: "/ujian/agenda", Also: []string{"/ujian/ruang"}})
	}
	items = append(items, subnav.Item{Label: "Sesi saya", Path: "/ujian/sesi-saya", Also: []string{"/ujian/sesi"}})
	if review && staff && a.CanAny(ActBank, Read) {
		items = append(items, subnav.Item{Label: "Telaah", Path: "/ujian/telaah"})
	}
	if a.CanAny(ActAgenda, Write) {
		items = append(items, subnav.Item{Label: "Pengaturan", Path: "/ujian/pengaturan"})
	}
	return append(items, subnav.Item{Label: "Bantuan", Path: "/ujian/bantuan", Also: []string{"/ujian/panduan"}, End: true})
}

// withNav memuat Actor pengguna sekali per request (dipakai ulang oleh
// RequireAccess dan handler) dan, untuk GET, menyimpan sub-navigasi modul.
func (h *Handler) withNav(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, _ := auth.CurrentUser(r.Context())
		a, err := LoadActor(r.Context(), h.db, id)
		if err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "load exam actor", err)
			return
		}
		ctx := context.WithValue(r.Context(), actorKey{}, a)
		if r.Method == http.MethodGet {
			review := false
			if a.CanAny(ActBank, Read) {
				s, err := LoadExamSettings(ctx, h.db, id.SchoolID)
				if err != nil {
					page.ServerError(h.render, h.logger, w, r, id, "load exam settings", err)
					return
				}
				review = s.Review
			}
			ctx = subnav.With(ctx, subnav.Mark(Nav(a, ModuleFrom(ctx), review), r.URL.Path))
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
