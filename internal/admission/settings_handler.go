package admission

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// Mode pola NIS di halaman pengaturan (T120).
const (
	nisModeYear     = "tahun"    // tahun + nomor urut
	nisModePrefix   = "awalan"   // awalan tetap + nomor urut
	nisModeContinue = "lanjut"   // lanjutkan dari NIS terakhir
	nisModeCustom   = "lanjutan" // pola token bebas
)

type settingsView struct {
	User           auth.Identity
	CanEdit        bool
	ReRegistration bool
	// Isian pembuat pola NIS.
	Mode       string
	YearDigits string // "4" atau "2"
	SeqDigits  int
	Prefix     string
	Custom     string
	// Year adalah tahun contoh (tahun mulai tahun ajaran pendaftaran
	// terdekat); LastNIS dan ContinueNext untuk mode lanjut.
	Year                  int
	LastNIS, ContinueNext string
	Errors                map[string]string
	Flash                 string
	Error                 string
}

var (
	simpleYearRe   = regexp.MustCompile(`^\{(yyyy|yy)\}\{seq:([1-6])\}$`)
	simplePrefixRe = regexp.MustCompile(`^([A-Za-z0-9./-]*)\{seq:([1-6])\}$`)
)

// describeNISFormat memecah pola tersimpan menjadi isian pembuat pola.
func describeNISFormat(format string, v *settingsView) {
	v.Mode, v.YearDigits, v.SeqDigits, v.Prefix, v.Custom = nisModeCustom, "4", 3, "", format
	if format == NISContinue {
		v.Mode, v.Custom = nisModeContinue, DefaultNISFormat
		return
	}
	if m := simpleYearRe.FindStringSubmatch(format); m != nil {
		v.Mode, v.SeqDigits = nisModeYear, atoi(m[2])
		if m[1] == "yy" {
			v.YearDigits = "2"
		}
		return
	}
	if m := simplePrefixRe.FindStringSubmatch(format); m != nil && m[1] != "" {
		v.Mode, v.Prefix, v.SeqDigits = nisModePrefix, m[1], atoi(m[2])
	}
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

// buildNISFormat menyusun pola dari isian pembuat pola.
func buildNISFormat(mode, yearDigits, seqDigits, prefix, custom string) (string, error) {
	seq := atoi(seqDigits)
	if mode == nisModeYear || mode == nisModePrefix {
		if seq < 2 || seq > 6 {
			return "", fieldErr("seq_digits", "Jumlah digit nomor urut 2–6.")
		}
	}
	tail := "{seq:" + strconv.Itoa(seq) + "}"
	switch mode {
	case nisModeYear:
		if yearDigits == "2" {
			return "{yy}" + tail, nil
		}
		return "{yyyy}" + tail, nil
	case nisModePrefix:
		prefix = strings.TrimSpace(prefix)
		if prefix == "" || !nisLiteralRe.MatchString(prefix) {
			return "", fieldErr("prefix", "Awalan wajib diisi, hanya huruf, angka, titik, garis miring, atau tanda hubung.")
		}
		return prefix + tail, nil
	case nisModeContinue:
		return NISContinue, nil
	case nisModeCustom:
		f := strings.TrimSpace(custom)
		if err := ValidateNISFormat(f); err != nil || f == NISContinue {
			return "", fieldErr("custom", "Pola harus berisi tepat satu {seq:N}, mis. {yyyy}{seq:3}; hanya huruf, angka, titik, garis miring, tanda hubung, {yyyy}, dan {yy}.")
		}
		return f, nil
	}
	return "", fieldErr("mode", "Pilih cara pembuatan NIS.")
}

// sampleYear adalah tahun mulai tahun ajaran periode pendaftaran terbaru,
// atau tahun depan jika belum ada periode.
func (h *Handler) sampleYear(ctx context.Context, schoolID string) int {
	var y int
	if err := h.db.QueryRow(ctx, `SELECT extract(year FROM y.start_date)::int FROM admission_periods p
		JOIN academic_years y ON y.id = p.academic_year_id WHERE p.school_id = $1 ORDER BY p.opens_on DESC LIMIT 1`, schoolID).Scan(&y); err != nil {
		return h.periods.Today().Year() + 1
	}
	return y
}

// settingsPage menampilkan pengaturan pendaftaran sekolah. Peran:
// admin_tu, kepala_sekolah (baca).
func (h *Handler) settingsPage(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	view := settingsView{}
	uses, err := UsesReRegistration(ctx, h.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "reregistration setting", err)
		return
	}
	format, err := NISFormat(ctx, h.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "nis format", err)
		return
	}
	view.ReRegistration = uses
	describeNISFormat(format, &view)
	h.renderSettings(w, r, id, http.StatusOK, view)
}

func (h *Handler) renderSettings(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, view settingsView) {
	ctx := r.Context()
	view.User, view.CanEdit = id, id.HasRole(auth.RoleAdminTU)
	if view.Flash == "" {
		view.Flash = auth.PopFlash(ctx, h.sessions)
	}
	view.Year = h.sampleYear(ctx, id.SchoolID)
	last, err := lastNumericNIS(ctx, h.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "last nis", err)
		return
	}
	if last != "" {
		view.LastNIS, view.ContinueNext = last, incrDigits(last)
	}
	h.render.Render(w, r, status, "admission/settings", view)
}

// saveSettings menyimpan pengaturan pendaftaran. Peran: admin_tu.
func (h *Handler) saveSettings(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	f := r.PostForm
	view := settingsView{ReRegistration: f.Get("reregistration") != "false", Mode: f.Get("mode"), YearDigits: f.Get("year_digits"),
		SeqDigits: atoi(f.Get("seq_digits")), Prefix: f.Get("prefix"), Custom: f.Get("custom")}
	format, err := buildNISFormat(view.Mode, view.YearDigits, f.Get("seq_digits"), view.Prefix, view.Custom)
	if err == nil {
		err = SaveAdmissionSettings(r.Context(), h.db, actorFrom(r, id), AdmissionSettings{ReRegistration: view.ReRegistration, NISFormat: format})
	}
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Pengaturan pendaftaran disimpan.")
		http.Redirect(w, r, applicantsPath+"/pengaturan", http.StatusSeeOther)
	case errors.As(err, &ve):
		view.Errors = ve.Fields
		h.renderSettings(w, r, id, http.StatusBadRequest, view)
	default:
		h.serverError(w, r, "save admission settings", err)
	}
}

// enrollOne menjadikan satu pendaftar diterima sebagai siswa dari halaman
// detailnya (T120). Peran: admin_tu.
func (h *Handler) enrollOne(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	applicantID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	opts := EnrollOptions{NIS: strings.TrimSpace(r.PostForm.Get("nis")), ClassID: r.PostForm.Get("class_id")}
	_, nis, name, err := h.enroller().Enroll(r.Context(), actorFrom(r, id), applicantID, opts)
	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, name+" sudah menjadi siswa dengan NIS "+nis+".")
		http.Redirect(w, r, applicantsPath+"/"+applicantID, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgApplicantGone)
	case errors.As(err, &ve):
		h.renderApplicant(w, r, id, applicantID, http.StatusBadRequest, "", ve.Fields, "")
	case errors.Is(err, ErrNotAccepted):
		h.renderApplicant(w, r, id, applicantID, http.StatusConflict, "", nil, "Pendaftar ini belum diterima atau sudah menjadi siswa. Muat ulang halaman.")
	default:
		h.serverError(w, r, "enroll applicant", err)
	}
}

// markReRegistered menandai pendaftar diterima sudah (atau belum) daftar
// ulang (T120). Peran: admin_tu.
func (h *Handler) markReRegistered(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	applicantID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	done := r.PostForm.Get("done") != "0"
	a, err := h.applicants.SetReRegistered(r.Context(), actorFrom(r, id), applicantID, done)
	switch {
	case err == nil:
		msg := a.FullName + " ditandai sudah daftar ulang."
		if !done {
			msg = "Tanda daftar ulang " + a.FullName + " dibatalkan."
		}
		auth.SetFlash(r.Context(), h.sessions, msg)
		http.Redirect(w, r, applicantsPath+"/"+applicantID, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgApplicantGone)
	case errors.Is(err, ErrNotAccepted):
		h.renderApplicant(w, r, id, applicantID, http.StatusConflict, "", nil, "Hanya pendaftar diterima yang belum menjadi siswa yang bisa ditandai daftar ulang.")
	default:
		h.serverError(w, r, "mark reregistered", err)
	}
}

// reRegisteredLabel memformat tanggal daftar ulang ("" jika belum).
func reRegisteredLabel(a Applicant) string {
	if a.ReRegisteredAt == nil {
		return ""
	}
	return clock.FormatDateID(a.ReRegisteredAt.In(clock.Jakarta))
}
