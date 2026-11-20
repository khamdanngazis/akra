package admission

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"

	"akra/internal/auth"
	"akra/internal/exam"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/page"
)

// Tes seleksi online (T149) dari sisi pendaftaran: kode kartu tes
// pendaftar memakai kunci yang sama dengan modul ujian sehingga kartu di
// halaman status sama dengan kartu cetak TU.

// examLink adalah dependensi ujian untuk handler pendaftaran.
type examLink struct {
	cards   *exam.Issuer
	baseURL string
}

func defaultExamLink() examLink {
	return examLink{cards: exam.NewIssuer().WithCardCodes(exam.DevTokenSigner())}
}

// WithExam memakai kunci t untuk kode kartu tes pendaftar dan baseURL
// untuk tautan login peserta.
func (h *Handler) WithExam(t *exam.TokenSigner, baseURL string) *Handler {
	h.exam = examLink{cards: exam.NewIssuer().WithCardCodes(t), baseURL: baseURL}
	return h
}

// WithExam memakai kunci t untuk kode kartu tes pendaftar dan baseURL
// untuk tautan login peserta.
func (h *PublicHandler) WithExam(t *exam.TokenSigner, baseURL string) *PublicHandler {
	h.exam = examLink{cards: exam.NewIssuer().WithCardCodes(t), baseURL: baseURL}
	return h
}

// staffActor membuat aktor modul ujian untuk TU (tindakan dari halaman
// pendaftaran).
func staffActor(r *http.Request, id auth.Identity) exam.StaffActor {
	return exam.StaffActor{Actor: exam.Actor{UserID: id.UserID, Roles: id.Roles}, SchoolID: id.SchoolID, IP: auth.ClientIP(r)}
}

// onlineTestRow adalah ringkasan tes online di tab Tes seleksi.
type onlineTestRow struct {
	exam.OnlineTest
	Sessions []sessionRow
	Open     []sessionRow
}

type sessionRow struct {
	exam.AdmissionSession
	When string
}

func sessionWhen(s exam.AdmissionSession, loc *time.Location) string {
	start, end := s.Start.In(loc), s.End.In(loc)
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return clock.FormatDateID(start) + ", " + start.Format("15.04") + "–" + end.Format("15.04")
	}
	return clock.FormatDateTimeID(start, loc) + " s.d. " + clock.FormatDateTimeID(end, loc)
}

// loadOnlineTest meringkas ujian tes online untuk gelombang.
func (h *Handler) loadOnlineTest(ctx context.Context, schoolID, examID, periodID string, loc *time.Location) (*onlineTestRow, error) {
	o, err := exam.LoadOnlineTest(ctx, h.db, schoolID, examID, periodID, time.Now())
	if err != nil {
		return nil, err
	}
	row := &onlineTestRow{OnlineTest: o}
	for _, s := range o.Sessions {
		sr := sessionRow{AdmissionSession: s, When: sessionWhen(s, loc)}
		row.Sessions = append(row.Sessions, sr)
		if !s.Ended {
			row.Open = append(row.Open, sr)
		}
	}
	return row, nil
}

// addApplicantsToSession memasukkan pendaftar terverifikasi yang belum
// punya sesi ke sesi tes online yang dipilih TU. Peran: admin_tu.
func (h *Handler) addApplicantsToSession(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID, testID := chi.URLParam(r, "id"), chi.URLParam(r, "testID")
	back := periodsPath + "/" + periodID + "/tes"
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	tests, err := h.selection().Tests(r.Context(), id.SchoolID, periodID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "list tests", err)
		return
	}
	var test *SelectionTest
	for i := range tests {
		if tests[i].ID == testID && tests[i].Online() {
			test = &tests[i]
		}
	}
	if test == nil {
		page.NotFound(h.render, w, r, id, "Tes online tidak ditemukan.")
		return
	}
	n, err := exam.AddMissingApplicants(r.Context(), h.db, staffActor(r, id), test.ExamID, r.PostForm.Get("session_id"), time.Now())
	switch {
	case errors.Is(err, exam.ErrNotFound):
		auth.SetFlash(r.Context(), h.sessions, "Pilih sesi tes online gelombang ini.")
	case errors.Is(err, exam.ErrSessionEnded):
		auth.SetFlash(r.Context(), h.sessions, "Sesi itu sudah berakhir. Buat sesi baru di ujiannya.")
	case err != nil:
		h.serverError(w, r, "add applicants", err)
		return
	case n == 0:
		auth.SetFlash(r.Context(), h.sessions, "Tidak ada pendaftar baru yang perlu dimasukkan.")
	default:
		auth.SetFlash(r.Context(), h.sessions, fmt.Sprintf("%d pendaftar dimasukkan ke sesi. Kartu tesnya tampil di halaman status pendaftaran.", n))
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// openPractice membuka ujian latihan untuk pendaftar gelombang. Peran:
// admin_tu.
func (h *Handler) openPractice(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	periodID := chi.URLParam(r, "id")
	back := periodsPath + "/" + periodID + "/tes"
	now := time.Now()
	if _, err := h.periods.Get(r.Context(), id.SchoolID, periodID); errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	} else if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	// Latihan sampai sesi tes pertama gelombang mulai (paling lama 7 hari).
	end := now.Add(exam.PracticeWindowApplicants)
	var first *time.Time
	if err := h.db.QueryRow(r.Context(), `SELECT min(x.starts_at) FROM exam_sessions x JOIN admission_tests t ON t.exam_id = x.exam_id
		WHERE t.school_id = $1 AND t.period_id = $2 AND x.period_id = $2 AND x.cancelled_at IS NULL AND x.starts_at > $3`,
		id.SchoolID, periodID, now).Scan(&first); err != nil {
		h.serverError(w, r, "first test session", err)
		return
	}
	if first != nil {
		end = *first
	}
	_, n, err := exam.OpenApplicantPractice(r.Context(), h.db, staffActor(r, id), periodID, end, now)
	switch {
	case errors.Is(err, exam.ErrNoPractice):
		auth.SetFlash(r.Context(), h.sessions, "Paket ujian latihan belum disiapkan. Buka menu Ujian → Ujian latihan sekali untuk menyiapkannya.")
	case errors.Is(err, exam.ErrNotFound):
		page.NotFound(h.render, w, r, id, msgPeriodGone)
		return
	case err != nil:
		h.serverError(w, r, "open applicant practice", err)
		return
	default:
		auth.SetFlash(r.Context(), h.sessions, fmt.Sprintf("Ujian latihan dibuka untuk pendaftar (%d baru). Tombol Coba ujian latihan tampil di kartu tes mereka.", n))
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// testCardView adalah kartu tes online di halaman status pendaftaran.
type testCardView struct {
	exam.ApplicantCard
	Tests      []testCardTest
	CodeFrom   string
	HasPending bool // ada tes/latihan yang belum berakhir
	Practice   bool // ada latihan yang masih buka
}

type testCardTest struct {
	exam.ApplicantTestCard
	When string
}

// loadTestCard menyusun kartu tes online pendaftar; nil bila tidak ada
// sesi online. Kode diterbitkan otomatis saat pertama kali tersedia.
func loadTestCard(ctx context.Context, db database.DB, l examLink, schoolID, applicantID string, loc *time.Location) (*testCardView, error) {
	c, err := exam.LoadApplicantCard(ctx, db, l.cards, schoolID, applicantID, l.baseURL, time.Now())
	if err != nil {
		return nil, err
	}
	if len(c.Tests) == 0 {
		return nil, nil
	}
	v := &testCardView{ApplicantCard: c}
	if !c.CodeFrom.IsZero() {
		v.CodeFrom = clock.FormatDateTimeID(c.CodeFrom, loc)
	}
	for _, t := range c.Tests {
		v.Tests = append(v.Tests, testCardTest{ApplicantTestCard: t,
			When: sessionWhen(exam.AdmissionSession{Start: t.Start, End: t.End}, loc)})
		if !t.Ended {
			v.HasPending = true
			v.Practice = v.Practice || t.Practice
		}
	}
	return v, nil
}

// ProofLines meringkas kartu tes online untuk bukti pendaftaran (PDF):
// satu baris per tes/latihan yang belum berakhir, lalu cara masuk.
func (v *testCardView) ProofLines() []string {
	if v == nil || !v.HasPending {
		return nil
	}
	var out []string
	for _, t := range v.Tests {
		if t.Ended {
			continue
		}
		where := "di sekolah"
		if t.Remote {
			where = "dari rumah"
		}
		name := t.ExamName
		if t.Practice {
			name = "Ujian latihan (coba tampilan)"
		}
		out = append(out, name+" (online, "+where+"): "+t.When)
	}
	line := "Masuk di " + v.LoginURL + " dengan nomor pendaftaran " + v.Identifier
	if v.Code != "" {
		line += " dan kode akses " + v.CodeText()
	} else if v.CodeFrom != "" {
		line += "; kode akses tampil di halaman status mulai " + v.CodeFrom
	}
	return append(out, line)
}

// CodeText menampilkan kode dalam dua kelompok.
func (v testCardView) CodeText() string { return codeGroups(v.Code) }

// QR mengembalikan gambar QR tautan login (PNG data URI) untuk dipindai
// HP; kode akses tidak pernah masuk QR.
func (v testCardView) QR() template.URL {
	png, err := qrcode.Encode(v.LoginURL, qrcode.Medium, 256)
	if err != nil {
		return ""
	}
	return template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png)) //nolint:gosec // PNG buatan sendiri
}

// codeGroups menulis kode dalam dua kelompok agar mudah dibaca, mis.
// "ABCD EFGH".
func codeGroups(code string) string {
	if len(code) <= 4 {
		return code
	}
	return code[:4] + " " + code[4:]
}
