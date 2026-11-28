package exam

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"akra/internal/auth"
	"akra/internal/duty"
	"akra/internal/platform/database"
	"akra/internal/platform/module"
	"akra/internal/platform/page"
)

// ModuleName adalah kode modul ujian di katalog modul dan menu.
const ModuleName = string(module.Exam)

// Release adalah tahap rilis modul ujian (indeks MVP 4 bagian 7).
type Release string

const (
	R1 Release = "r1" // tes seleksi online
	R2 Release = "r2" // ulangan guru
	R3 Release = "r3" // PTS/PAS sekolah
)

var releaseRank = map[Release]int{R1: 1, R2: 2, R3: 3}

// ValidRelease melaporkan apakah r adalah tahap rilis yang dikenal.
func ValidRelease(r Release) bool { return releaseRank[r] > 0 }

// Module adalah status aktivasi modul ujian satu sekolah.
type Module struct {
	Enabled bool
	Release Release
	// Admission dan Grades: modul pendaftaran dan nilai aktif untuk sekolah
	// (T166). Tujuan "Tes seleksi" butuh pendaftaran; kirim nilai ke rapor
	// butuh nilai.
	Admission, Grades bool
}

// Offers melaporkan apakah ujian bertujuan p bisa dibuat di sekolah ini:
// tahap rilisnya sudah dibuka dan modul pasangannya aktif.
func (m Module) Offers(p Purpose) bool {
	return m.Has(p.Release()) && (p != PurposeAdmission || m.Admission)
}

// Has melaporkan apakah modul aktif dan tahap r sudah dibuka.
func (m Module) Has(r Release) bool {
	return m.Enabled && releaseRank[m.Release] >= releaseRank[r]
}

// LoadModule membaca status modul ujian sekolah dari status modul sekolah
// (T163; cache per replika, lihat module.Load). Belum diatur berarti
// nonaktif; tahap tidak dikenal dianggap error agar salah ketik tidak
// diam-diam membuka fitur.
func LoadModule(ctx context.Context, q database.DBTX, schoolID string) (Module, error) {
	set, err := module.Load(ctx, q, schoolID)
	if err != nil {
		return Module{}, err
	}
	return moduleOf(set)
}

// moduleOf mengambil status modul ujian dari set modul sekolah.
func moduleOf(set module.Set) (Module, error) {
	m := Module{Enabled: set.Has(module.Exam), Release: R1, Admission: set.Has(module.Admission), Grades: set.Has(module.Grades)}
	if rel := set.Option(module.Exam, "release"); rel != "" {
		m.Release = Release(rel)
		if !ValidRelease(m.Release) {
			return Module{}, fmt.Errorf("exam: invalid release %q", rel)
		}
	}
	return m, nil
}

type moduleKey struct{}

// ModuleFrom mengembalikan status modul yang disimpan RequireModule.
func ModuleFrom(ctx context.Context) Module {
	m, _ := ctx.Value(moduleKey{}).(Module)
	return m
}

// RequireModule mengembalikan middleware yang menjawab 404 bila modul ujian
// nonaktif atau tahap rel belum dibuka untuk sekolah pengguna. Menu juga
// disembunyikan, tetapi route tetap dilindungi di sini. Pasang di dalam
// grup RequireLogin.
func RequireModule(db database.DBTX, rd page.Renderer, logger *slog.Logger, rel Release) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := auth.CurrentUser(r.Context())
			m, err := LoadModule(r.Context(), db, id.SchoolID)
			if err != nil {
				page.ServerError(rd, logger, w, r, id, "load exam module", err)
				return
			}
			if !m.Has(rel) {
				page.NotFound(rd, w, r, id, "Halaman tidak ditemukan.")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), moduleKey{}, m)))
		})
	}
}

// LoadActor membaca fakta akses pengguna id dari database: tugas
// koordinator, mapel/kelas yang diampu di tahun ajaran aktif, dan
// penugasan pengawas sesi ujian.
func LoadActor(ctx context.Context, q database.DBTX, id auth.Identity) (Actor, error) {
	a := Actor{UserID: id.UserID, Roles: id.Roles}
	var err error
	if a.Proctors, err = loadProctorAssignments(ctx, q, id.SchoolID, id.UserID, time.Now()); err != nil {
		return Actor{}, err
	}
	if !id.HasRole(roleTeacher) {
		return a, nil
	}
	if a.Coordinator, err = duty.HasActive(ctx, q, id.SchoolID, id.UserID, duty.ExamCoordinator); err != nil {
		return Actor{}, err
	}
	rows, err := q.Query(ctx, `
		SELECT ta.class_id, ta.subject_id FROM teaching_assignments ta
		JOIN academic_years y ON y.id = ta.academic_year_id AND y.school_id = ta.school_id AND y.is_active
		WHERE ta.school_id = $1 AND ta.teacher_id = $2`, id.SchoolID, id.UserID)
	if err != nil {
		return Actor{}, fmt.Errorf("exam: load assignments: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var classID, subjectID string
		if err := rows.Scan(&classID, &subjectID); err != nil {
			return Actor{}, fmt.Errorf("exam: load assignments: %w", err)
		}
		a.Classes = append(a.Classes, classID+"|"+subjectID)
		if !seen[subjectID] {
			seen[subjectID] = true
			a.Subjects = append(a.Subjects, subjectID)
		}
	}
	return a, rows.Err()
}

type actorKey struct{}

// ActorFrom mengembalikan Actor yang disimpan RequireAccess.
func ActorFrom(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(actorKey{}).(Actor)
	return a, ok
}

// ErrForbidden dikembalikan bila pengguna tidak punya akses.
var ErrForbidden = errors.New("exam: forbidden")

// RequireAccess mengembalikan middleware yang memuat Actor pengguna dan
// menolak dengan 403 bila ia tidak punya akses minimal want ke aksi act
// untuk sumber daya apa pun. Cek per sumber daya (mapel, sesi) tetap
// dilakukan handler dengan Actor.Can.
func RequireAccess(db database.DBTX, rd page.Renderer, logger *slog.Logger, act Action, want Level) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, _ := auth.CurrentUser(r.Context())
			a, ok := ActorFrom(r.Context())
			if !ok {
				var err error
				if a, err = LoadActor(r.Context(), db, id); err != nil {
					page.ServerError(rd, logger, w, r, id, "load exam actor", err)
					return
				}
			}
			if !a.CanAny(act, want) {
				logger.WarnContext(r.Context(), "exam access denied", "user_id", id.UserID, "action", string(act), "path", r.URL.Path)
				page.Forbidden(rd, w, r, id, "Akun Anda tidak memiliki akses ke bagian ujian ini.")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
		})
	}
}
