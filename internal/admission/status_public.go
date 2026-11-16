package admission

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
)

const sessionStatus = "admission_status" // "<school_id>|<applicant_id>"

// statusExplain menjelaskan arti status untuk orang tua.
var statusExplain = map[string]string{
	ApplicantSubmitted: "Pendaftaran sudah diterima sekolah dan sedang diperiksa.",
	ApplicantFix:       "Sekolah meminta perbaikan data. Lihat catatan di bawah, lalu lanjutkan formulir dengan kode lanjut dari sekolah.",
	ApplicantVerified:  "Data sudah diperiksa dan dinyatakan lengkap.",
	ApplicantAccepted:  "Selamat, calon siswa diterima. Sekolah akan menghubungi Anda untuk langkah berikutnya.",
	ApplicantReserve:   "Calon siswa masuk daftar cadangan. Sekolah akan menghubungi jika ada kursi kosong.",
	ApplicantRejected:  "Mohon maaf, calon siswa belum dapat diterima pada periode ini.",
	ApplicantWithdrawn: "Pendaftaran ini sudah dibatalkan.",
}

// FindForStatus mencari pendaftar yang sudah dikirim berdasarkan nomor
// pendaftaran dan tanggal lahir. ErrNotFound untuk kombinasi yang salah.
func (s *ApplicantService) FindForStatus(ctx context.Context, schoolID, number, birthDate string) (Applicant, error) {
	number = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(number), " ", ""))
	birth, err := time.Parse(dateLayout, strings.TrimSpace(birthDate))
	if number == "" || err != nil {
		return Applicant{}, ErrNotFound
	}
	var id string
	err = s.db.QueryRow(ctx, `SELECT id FROM applicants WHERE school_id = $1 AND registration_number = $2 AND birth_date = $3 AND status <> 'draf'
		  AND anonymized_at IS NULL`,
		schoolID, number, birth).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Applicant{}, ErrNotFound
	}
	if err != nil {
		return Applicant{}, mapApplicantError(err)
	}
	return s.Load(ctx, schoolID, id)
}

// proofData menyusun isi bukti pendaftaran.
func proofData(ctx context.Context, h *Handler, schoolName string, a Applicant) (ProofData, error) {
	p, err := h.periods.Get(ctx, a.SchoolID, a.PeriodID)
	if err != nil {
		return ProofData{}, err
	}
	loc, err := clock.ForSchool(ctx, h.db, a.SchoolID)
	if err != nil {
		return ProofData{}, err
	}
	d := ProofData{SchoolName: schoolName, RegistrationNumber: a.RegistrationNumber, FullName: a.FullName, BirthDate: a.BirthDate,
		PeriodName: p.Name, YearName: p.AcademicYearName, Status: StatusLabels[a.Status],
		VerificationCode: VerificationCode(a.ID, a.RegistrationNumber), PrintedAt: time.Now().In(loc)}
	if a.SubmittedAt != nil {
		d.SubmittedAt = a.SubmittedAt.In(loc)
	}
	usesTest := false
	for _, t := range p.Tracks {
		if t.ID == a.TrackID {
			d.TrackName, usesTest = t.Name, t.UsesTest
		}
	}
	docs, err := h.documents.List(ctx, a.SchoolID, a.ID)
	if err != nil {
		return ProofData{}, err
	}
	for _, doc := range docs {
		d.Documents = append(d.Documents, doc.Label)
	}
	if usesTest {
		d.Tests, err = testSchedule(ctx, h.selection(), a.SchoolID, p.ID)
		if err != nil {
			return ProofData{}, err
		}
	}
	return d, nil
}

func testSchedule(ctx context.Context, sel *SelectionService, schoolID, periodID string) ([]string, error) {
	tests, err := sel.Tests(ctx, schoolID, periodID)
	if err != nil {
		return nil, err
	}
	loc, err := clock.ForSchool(ctx, sel.db, schoolID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, t := range tests {
		line := t.Name + ": jadwal menyusul"
		if t.ScheduledAt != nil {
			line = t.Name + ": " + clock.FormatDateTimeID(*t.ScheduledAt, loc)
		}
		if t.Location != "" {
			line += ", " + t.Location
		}
		out = append(out, line)
	}
	return out, nil
}

// staffProof mengunduh bukti pendaftaran. Peran: admin_tu, kepala_sekolah.
func (h *Handler) staffProof(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	a, err := h.applicants.Load(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) || (err == nil && a.RegistrationNumber == "") {
		page.NotFound(h.render, w, r, id, msgApplicantGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "load applicant", err)
		return
	}
	d, err := proofData(r.Context(), h, id.SchoolName, a)
	if err != nil {
		h.serverError(w, r, "proof data", err)
		return
	}
	writeProof(w, d, func(err error) { h.serverError(w, r, "write proof", err) })
}

func writeProof(w http.ResponseWriter, d ProofData, fail func(error)) {
	var buf bytes.Buffer
	if err := WriteProofPDF(&buf, d, true); err != nil {
		fail(err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="bukti-`+strings.ReplaceAll(d.RegistrationNumber, "/", "-")+`.pdf"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write(buf.Bytes())
}

type statusView struct {
	School      schoolInfo
	Found       bool
	Number      string
	BirthDate   string
	Applicant   Applicant
	StatusLabel string
	Tone        string
	History     []historyItem
	Explain     string
	Note        string
	Period      string
	Track       string
	Tests       []string
	MissingDocs []string
	FeeAmount   string
	FeePaid     bool
	Steps       []stepNav
	StepName    string
	Error       string
	Flash       string
}

// statusPage menampilkan form cek status, atau status pendaftar jika sesi
// sudah membuktikan nomor + tanggal lahir.
func (h *PublicHandler) statusPage(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	view := statusView{School: s, StepName: "Cek status pendaftaran", Flash: auth.PopFlash(r.Context(), h.sessions)}
	school, id, _ := strings.Cut(h.sessions.GetString(r.Context(), sessionStatus), "|")
	if school == s.ID && id != "" {
		a, err := h.applicants.Load(r.Context(), s.ID, id)
		if err == nil {
			if err := h.fillStatus(r.Context(), s, a, &view); err != nil {
				h.serverError(w, r, "status view", err)
				return
			}
		} else if !errors.Is(err, ErrNotFound) {
			h.serverError(w, r, "load applicant", err)
			return
		}
	}
	h.render.Render(w, r, http.StatusOK, "admission/public_status", view)
}

func (h *PublicHandler) fillStatus(ctx context.Context, s schoolInfo, a Applicant, view *statusView) error {
	view.Found, view.Applicant = true, a
	view.StatusLabel, view.Explain, view.Tone = StatusLabels[a.Status], statusExplain[a.Status], statusTone(a.Status)
	events, err := h.applicants.Events(ctx, s.ID, a.ID)
	if err != nil {
		return err
	}
	view.History = publicHistory(events)
	p, err := h.periods.Get(ctx, s.ID, a.PeriodID)
	if err != nil {
		return err
	}
	view.Period = p.Name
	usesTest := false
	for _, t := range p.Tracks {
		if t.ID == a.TrackID {
			view.Track, usesTest = t.Name, t.UsesTest
		}
	}
	if a.Status == ApplicantFix {
		if view.Note, err = h.applicants.LastNote(ctx, s.ID, a.ID); err != nil {
			return err
		}
	}
	if a.Editable() {
		docs, err := h.documents.List(ctx, s.ID, a.ID)
		if err != nil {
			return err
		}
		for _, k := range MissingDocs(p, docs) {
			view.MissingDocs = append(view.MissingDocs, DocLabel(k))
		}
	}
	if usesTest && (a.Status == ApplicantVerified || a.Status == ApplicantSubmitted) {
		if view.Tests, err = testSchedule(ctx, NewSelectionService(h.db), s.ID, p.ID); err != nil {
			return err
		}
	}
	var fee int64
	if err := h.db.QueryRow(ctx, `SELECT fee_amount FROM applicants WHERE id = $1`, a.ID).Scan(&fee); err != nil {
		return err
	}
	if fee > 0 {
		view.FeeAmount = money.FormatRupiah(fee)
		if view.FeePaid, err = feePaid(ctx, h.db, a.ID); err != nil {
			return err
		}
	}
	return nil
}

// checkStatus memeriksa nomor + tanggal lahir lalu menyimpan buktinya di
// sesi. Pesan error sama untuk nomor yang salah dan tanggal lahir yang
// salah.
func (h *PublicHandler) checkStatus(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxPublicForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	number, birth := r.PostForm.Get("number"), r.PostForm.Get("birth_date")
	key := strings.ToUpper(strings.TrimSpace(number))
	ip := auth.ClientIP(r)
	view := statusView{School: s, Number: number, BirthDate: birth, StepName: "Cek status pendaftaran"}
	allowed, err := h.limiter.Allow(r.Context(), attemptStatus, key, ip)
	if err != nil {
		h.serverError(w, r, "check status limit", err)
		return
	}
	if !allowed {
		view.Error = "Terlalu banyak percobaan. Coba lagi dalam 15 menit."
		h.render.Render(w, r, http.StatusTooManyRequests, "admission/public_status", view)
		return
	}
	a, err := h.applicants.FindForStatus(r.Context(), s.ID, number, birth)
	_ = h.limiter.Record(r.Context(), s.ID, attemptStatus, key, ip, err == nil)
	switch {
	case err == nil:
		if err := h.sessions.RenewToken(r.Context()); err != nil {
			h.serverError(w, r, "renew session", err)
			return
		}
		h.sessions.Put(r.Context(), sessionStatus, s.ID+"|"+a.ID)
		http.Redirect(w, r, "/s/"+s.Code+"/status", http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		view.Error = "Nomor pendaftaran atau tanggal lahir tidak cocok."
		h.render.Render(w, r, http.StatusUnauthorized, "admission/public_status", view)
	default:
		h.serverError(w, r, "find applicant", err)
	}
}

// statusLogout menghapus akses status dari sesi.
func (h *PublicHandler) statusLogout(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	h.sessions.Remove(r.Context(), sessionStatus)
	http.Redirect(w, r, "/s/"+s.Code+"/status", http.StatusSeeOther)
}

// statusProof mengunduh bukti pendaftaran untuk pendaftar di sesi.
func (h *PublicHandler) statusProof(w http.ResponseWriter, r *http.Request) {
	s, ok := h.resolveSchool(w, r)
	if !ok {
		return
	}
	school, id, _ := strings.Cut(h.sessions.GetString(r.Context(), sessionStatus), "|")
	if school != s.ID || id == "" {
		http.Redirect(w, r, "/s/"+s.Code+"/status", http.StatusSeeOther)
		return
	}
	a, err := h.applicants.Load(r.Context(), s.ID, id)
	if err != nil {
		h.serverError(w, r, "load applicant", err)
		return
	}
	staff := &Handler{db: h.db, periods: h.periods, applicants: h.applicants, documents: h.documents}
	d, err := proofData(r.Context(), staff, s.Name, a)
	if err != nil {
		h.serverError(w, r, "proof data", err)
		return
	}
	writeProof(w, d, func(err error) { h.serverError(w, r, "write proof", err) })
}
