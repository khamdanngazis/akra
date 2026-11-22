package exam

import (
	"bytes"
	"errors"
	"html/template"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
)

// docsRoutes mendaftarkan dokumen ujian (T135). readers: siapa pun yang
// boleh membuka ujian; writers: pengelola ujian (kartu berisi kode, naskah,
// dan kunci).
func (h *Handler) examDocsRoutes(readers, writers chi.Router) {
	readers.Get("/paket/{id}/daftar-hadir.pdf", h.examAttendancePDF)
	readers.Get("/paket/{id}/berita-acara.pdf", h.examMinutesPDF)
	writers.Post("/paket/{id}/kartu", h.examCards)
	writers.Get("/paket/{id}/naskah", h.examPaper)
	writers.Get("/paket/{id}/kunci", h.examKey)
}

// WithBaseURL mengatur URL publik untuk QR kartu peserta.
func (h *Handler) WithBaseURL(u string) *Handler {
	h.baseURL = u
	return h
}

func (h *Handler) writePDF(w http.ResponseWriter, r *http.Request, name string, write func(*bytes.Buffer) error) {
	var buf bytes.Buffer
	if err := write(&buf); err != nil {
		h.fail(w, r, "write pdf", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+safeFileName(name)+`.pdf"`)
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(buf.Bytes())
}

// docsFail menangani error dokumen sebagai flash di tab Dokumen.
func (h *Handler) docsFail(w http.ResponseWriter, r *http.Request, back, op string, err error) {
	var te *TooEarlyError
	id, _ := auth.CurrentUser(r.Context())
	switch {
	case errors.As(err, &te):
		h.flash(r, "Kartu baru bisa dicetak mulai "+clock.FormatDateTimeID(te.Opens, id.Location())+" (H-3), agar kode tidak beredar terlalu lama.")
	case errors.Is(err, ErrCardsIssued):
		h.flash(r, "Sebagian peserta sudah punya kartu. Centang konfirmasi untuk mencetak ulang: kode baru dibuat dan kartu lama tidak berlaku lagi.")
	case errors.Is(err, ErrNoSessions):
		h.flash(r, "Tidak ada sesi dengan peserta untuk dicetak.")
	default:
		h.fail(w, r, op, err)
		return
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

func (h *Handler) examCards(w http.ResponseWriter, r *http.Request) {
	_, sa, e, ok := h.examWrite(w, r)
	if !ok {
		return
	}
	back := examPath(e.ID) + "?tab=dokumen"
	now := time.Now()
	sc := CardScope{ExamID: e.ID, SessionID: r.PostForm.Get("session_id")}
	cards, err := IssueCards(r.Context(), h.db, sa, h.issuer, sc, e.Name, h.baseURL, r.PostForm.Get("ulang") != "", now)
	if err != nil {
		h.docsFail(w, r, back, "issue cards", err)
		return
	}
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return
	}
	h.writePDF(w, r, "kartu-peserta-"+e.Name, func(b *bytes.Buffer) error { return WriteCardsPDFWith(b, lh, cards, now, true, cardOptions(r)) })
}

func (h *Handler) sessionDocs(w http.ResponseWriter, r *http.Request, examID, eventID string) ([]SessionDoc, Letterhead, bool) {
	_, sa := h.staff(r)
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return nil, lh, false
	}
	q := r.URL.Query()
	docs, err := LoadSessionDocs(r.Context(), h.db, sa.SchoolID, examID, eventID, q.Get("ruang"), q.Get("sesi"), lh.Loc)
	if err != nil {
		back := examPath(examID) + "?tab=dokumen"
		if eventID != "" {
			back = eventPath(eventID) + "/dokumen"
		}
		h.docsFail(w, r, back, "session docs", err)
		return nil, lh, false
	}
	return docs, lh, true
}

// minutesFor mengisi berita acara sesi yang sudah mulai dari data
// pelaksanaan (T143); sesi yang belum mulai tetap berkolom kosong untuk
// diisi tangan.
func (h *Handler) minutesFor(w http.ResponseWriter, r *http.Request, docs []SessionDoc, lh Letterhead) (map[string]SessionAttendance, bool) {
	_, sa := h.staff(r)
	now := time.Now()
	out := map[string]SessionAttendance{}
	for _, d := range docs {
		if now.Before(d.Session.StartsAt) {
			continue
		}
		m, err := LoadMinutes(r.Context(), h.db, sa.SchoolID, d.Session.ID, lh.Loc)
		if err != nil {
			h.fail(w, r, "load minutes", err)
			return nil, false
		}
		at := m.SessionAttendance
		at.SignedBy, at.SignedAt = m.SignedBy, m.SignedAt
		out[d.Session.ID] = at
	}
	return out, true
}

func (h *Handler) examAttendancePDF(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Read)
	if !ok {
		return
	}
	docs, lh, ok := h.sessionDocs(w, r, e.ID, "")
	if !ok {
		return
	}
	h.writePDF(w, r, "daftar-hadir-"+e.Name, func(b *bytes.Buffer) error { return WriteAttendancePDF(b, lh, docs, time.Now(), true) })
}

func (h *Handler) examMinutesPDF(w http.ResponseWriter, r *http.Request) {
	_, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Read)
	if !ok {
		return
	}
	docs, lh, ok := h.sessionDocs(w, r, e.ID, "")
	if !ok {
		return
	}
	att, ok := h.minutesFor(w, r, docs, lh)
	if !ok {
		return
	}
	h.writePDF(w, r, "berita-acara-"+e.Name, func(b *bytes.Buffer) error { return WriteMinutesPDF(b, lh, docs, att, time.Now(), true) })
}

// paperQuestion adalah satu soal di naskah cetak.
type paperQuestion struct {
	Section    *Section
	First      bool // soal pertama bagiannya
	Stimulus   bool // soal pertama kelompok stimulus
	StimTitle  string
	StimHTML   template.HTML
	View       QuestionView
	Key        []string
	Points     int
	InStimulus bool
}

type paperView struct {
	User      auth.Identity
	Exam      Exam
	Head      Letterhead
	Variant   Variant
	Variants  []Variant
	Items     []paperQuestion
	Numbers   []paperAnswer
	PrintedAt string
	IsKey     bool
}

// paperAnswer adalah satu baris lembar jawaban.
type paperAnswer struct {
	Number  int
	Letters []string // opsi pilihan ganda
	Kind    Kind
}

func (h *Handler) loadPaperView(w http.ResponseWriter, r *http.Request, isKey bool) (paperView, bool) {
	id, sa := h.staff(r)
	e, ok := h.loadExam(w, r, sa, Write)
	if !ok {
		return paperView{}, false
	}
	ctx := r.Context()
	vs, err := ListVariants(ctx, h.db, sa.SchoolID, e.ID)
	if err != nil || len(vs) == 0 {
		h.fail(w, r, "list variants", err)
		return paperView{}, false
	}
	lh, err := LoadLetterhead(ctx, h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return paperView{}, false
	}
	v := paperView{User: id, Exam: e, Head: lh, Variants: vs, Variant: vs[0], IsKey: isKey, PrintedAt: clock.FormatDateID(time.Now().In(lh.Loc))}
	for _, x := range vs {
		if x.Label == r.URL.Query().Get("paket") {
			v.Variant = x
		}
	}
	items, err := LoadPaper(ctx, h.db, e, v.Variant.ID)
	if err != nil {
		h.fail(w, r, "load paper", err)
		return paperView{}, false
	}
	secs, err := LoadContent(ctx, h.db, e)
	if err != nil {
		h.fail(w, r, "load sections", err)
		return paperView{}, false
	}
	secByID := map[string]*Section{}
	for i := range secs {
		secByID[secs[i].ID] = &secs[i]
	}
	prevSec := ""
	for _, it := range items {
		pq := ForParticipant(it.Question)
		pq.Points = it.Points
		q := paperQuestion{View: NewQuestionView(pq, it.Number, staffAssetURL), Points: it.Points, InStimulus: it.Stimulus != nil}
		if it.SectionID != prevSec {
			q.Section, q.First = secByID[it.SectionID], true
			prevSec = it.SectionID
		}
		if it.Stimulus != nil && it.FirstOfStimulus {
			q.Stimulus, q.StimTitle, q.StimHTML = true, it.Stimulus.Title, trusted(it.Stimulus.BodyHTML, staffAssetURL)
		}
		if isKey {
			q.Key = keyLines(it.Question)
		}
		v.Items = append(v.Items, q)
		a := paperAnswer{Number: it.Number, Kind: it.Question.Kind}
		if it.Question.Kind == KindMC || it.Question.Kind == KindMulti {
			for i := range it.Question.Options.Choices {
				a.Letters = append(a.Letters, letter(i))
			}
		}
		v.Numbers = append(v.Numbers, a)
	}
	return v, true
}

// examPaper menampilkan naskah cadangan (tanpa kunci) dan lembar jawaban
// untuk dicetak dari browser.
func (h *Handler) examPaper(w http.ResponseWriter, r *http.Request) {
	v, ok := h.loadPaperView(w, r, false)
	if !ok {
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/paper_print", v)
}

// examKey menampilkan kunci jawaban untuk pengelola ujian; setiap
// pembukaan tercatat di audit.
func (h *Handler) examKey(w http.ResponseWriter, r *http.Request) {
	v, ok := h.loadPaperView(w, r, true)
	if !ok {
		return
	}
	_, sa := h.staff(r)
	err := pgx.BeginFunc(r.Context(), h.db, func(tx pgx.Tx) error {
		return recordAudit(r.Context(), tx, sa, "exam.key_print", "exam", v.Exam.ID, nil, map[string]any{"variant": v.Variant.Label, "questions": len(v.Items)})
	})
	if err != nil {
		h.fail(w, r, "audit key print", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/key_print", v)
}

// docSession adalah satu sesi di tab Dokumen ujian.
type docSession struct {
	sessionView
	Issued    int
	CardsOpen bool
	OpensAt   string
}

// examDocSessions melengkapi sesi dengan status kartu.
func (h *Handler) examDocSessions(r *http.Request, sessions []sessionView, loc *time.Location, now time.Time) []docSession {
	var out []docSession
	for _, s := range sessions {
		d := docSession{sessionView: s, CardsOpen: s.StartsAt.Sub(now) <= CardLead, OpensAt: clock.FormatDateTimeID(s.StartsAt.Add(-CardLead), loc)}
		_ = h.db.QueryRow(r.Context(), `SELECT count(DISTINCT coalesce(student_id, applicant_id)) FROM student_credentials
			WHERE exam_session_id = $1 AND revoked_at IS NULL`, s.ID).Scan(&d.Issued)
		out = append(out, d)
	}
	return out
}

// Dokumen agenda PTS/PAS.

func (h *Handler) eventDocsRoutes(readers, writers chi.Router) {
	readers.Get("/agenda/{id}/dokumen", h.eventDetail)
	readers.Get("/agenda/{id}/daftar-hadir.pdf", h.eventAttendancePDF)
	readers.Get("/agenda/{id}/berita-acara.pdf", h.eventMinutesPDF)
	readers.Get("/agenda/{id}/denah.pdf", h.eventSeatingPDF)
	readers.Get("/agenda/{id}/pintu.pdf", h.eventDoorPDF)
	writers.Post("/agenda/{id}/kartu", h.eventCards)
	writers.Post("/agenda/{id}/duduk", h.eventReseat)
}

func (h *Handler) eventForDoc(w http.ResponseWriter, r *http.Request) (Event, bool) {
	_, sa := h.staff(r)
	ev, err := GetEvent(r.Context(), h.db, sa.SchoolID, chi.URLParam(r, "id"))
	if err != nil {
		h.fail(w, r, "get event", err)
		return Event{}, false
	}
	return ev, true
}

func (h *Handler) eventAttendancePDF(w http.ResponseWriter, r *http.Request) {
	ev, ok := h.eventForDoc(w, r)
	if !ok {
		return
	}
	docs, lh, ok := h.sessionDocs(w, r, "", ev.ID)
	if !ok {
		return
	}
	h.writePDF(w, r, "daftar-hadir-"+ev.Name, func(b *bytes.Buffer) error { return WriteAttendancePDF(b, lh, docs, time.Now(), true) })
}

func (h *Handler) eventMinutesPDF(w http.ResponseWriter, r *http.Request) {
	ev, ok := h.eventForDoc(w, r)
	if !ok {
		return
	}
	docs, lh, ok := h.sessionDocs(w, r, "", ev.ID)
	if !ok {
		return
	}
	att, ok := h.minutesFor(w, r, docs, lh)
	if !ok {
		return
	}
	h.writePDF(w, r, "berita-acara-"+ev.Name, func(b *bytes.Buffer) error { return WriteMinutesPDF(b, lh, docs, att, time.Now(), true) })
}

func (h *Handler) roomPlans(w http.ResponseWriter, r *http.Request) (Event, []RoomPlan, Letterhead, bool) {
	ev, ok := h.eventForDoc(w, r)
	if !ok {
		return ev, nil, Letterhead{}, false
	}
	_, sa := h.staff(r)
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return ev, nil, lh, false
	}
	plans, err := LoadRoomPlans(r.Context(), h.db, ev, r.URL.Query().Get("ruang"))
	if err != nil {
		h.docsFail(w, r, eventPath(ev.ID)+"/dokumen", "room plans", err)
		return ev, nil, lh, false
	}
	return ev, plans, lh, true
}

func (h *Handler) eventSeatingPDF(w http.ResponseWriter, r *http.Request) {
	ev, plans, lh, ok := h.roomPlans(w, r)
	if !ok {
		return
	}
	h.writePDF(w, r, "denah-"+ev.Name, func(b *bytes.Buffer) error { return WriteSeatingPDF(b, lh, ev.Name, plans, time.Now(), true) })
}

func (h *Handler) eventDoorPDF(w http.ResponseWriter, r *http.Request) {
	ev, plans, lh, ok := h.roomPlans(w, r)
	if !ok {
		return
	}
	h.writePDF(w, r, "tempelan-pintu-"+ev.Name, func(b *bytes.Buffer) error { return WriteDoorSignPDF(b, lh, ev.Name, plans, time.Now(), true) })
}

func (h *Handler) eventCards(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	now := time.Now()
	sc := CardScope{EventID: ev.ID, RoomID: r.PostForm.Get("room_id"), ClassID: r.PostForm.Get("class_id")}
	cards, err := IssueCards(r.Context(), h.db, sa, h.issuer, sc, ev.Name, h.baseURL, r.PostForm.Get("ulang") != "", now)
	if err != nil {
		h.docsFail(w, r, eventPath(ev.ID)+"/dokumen", "issue event cards", err)
		return
	}
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return
	}
	name := "kartu-peserta-" + ev.Name
	if sc.RoomID != "" || sc.ClassID != "" {
		name += "-" + strconv.Itoa(len(cards))
	}
	h.writePDF(w, r, name, func(b *bytes.Buffer) error { return WriteCardsPDFWith(b, lh, cards, now, true, cardOptions(r)) })
}

func (h *Handler) eventReseat(w http.ResponseWriter, r *http.Request) {
	sa, ev, ok := h.eventWrite(w, r)
	if !ok {
		return
	}
	interleave := r.PostForm.Get("selang") != ""
	if err := ReseatEvent(r.Context(), h.db, sa, ev.ID, interleave); err != nil {
		h.eventFail(w, r, eventPath(ev.ID)+"/dokumen", "reseat", err)
		return
	}
	if interleave {
		h.flash(r, "Tempat duduk diatur berselang antarkelas. Terapkan ke sesi agar paket soal ikut menyesuaikan.")
	} else {
		h.flash(r, "Tempat duduk diurutkan per kelas.")
	}
	http.Redirect(w, r, eventPath(ev.ID)+"/dokumen", http.StatusSeeOther)
}

// cardOptions membaca pilihan cetak kartu dari form: panduan peserta di
// balik kartu (T154).
func cardOptions(r *http.Request) CardOptions {
	return CardOptions{Guide: r.PostForm.Get("panduan") != ""}
}
