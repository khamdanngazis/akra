package exam

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
	"akra/internal/platform/storage"
)

// Halaman pengerjaan ujian peserta (T138): konfirmasi, mulai, mengerjakan,
// simpan, kumpul, selesai.

// WithStorage memasang penyimpanan gambar soal.
func (h *ParticipantHandler) WithStorage(s storage.Store) *ParticipantHandler {
	h.store = s
	return h
}

func (h *ParticipantHandler) takeRoutes(r chi.Router) {
	r.Get("/ujian/{session}", h.confirm)
	r.Post("/ujian/{session}/mulai", h.start)
	r.Get("/a/{attempt}", h.take)
	r.Post("/a/{attempt}/kumpul", h.submit)
	r.Get("/a/{attempt}/selesai", h.done)
	r.Get("/a/{attempt}/gambar/{asset}", h.image)
}

// WithTokens memasang penanda tangan token attempt (T139).
func (h *ParticipantHandler) WithTokens(t *TokenSigner) *ParticipantHandler {
	h.tokens = t
	return h
}

// TokenRoutes mendaftarkan route bertoken attempt. Dipasang di luar
// middleware sesi dan CSRF: ribuan peserta menyimpan jawaban bersamaan,
// dan membaca sesi di setiap simpan tidak layak (ADR 0001 bagian 4).
func (h *ParticipantHandler) TokenRoutes(r chi.Router) {
	// Peserta (token attempt di header): simpan jawaban.
	r.Post("/u/a/{attempt}/simpan", h.save)
}

func (h *ParticipantHandler) deviceID(r *http.Request) string {
	if c, err := r.Cookie(DeviceCookieName); err == nil {
		return c.Value
	}
	return ""
}

// clockLabel memformat jam (dan tanggal bila bukan hari ini) di zona sekolah.
func clockLabel(t, now time.Time, loc *time.Location) string {
	l := t.In(loc)
	s := l.Format("15.04") + " " + clock.ZoneLabel(loc)
	if !clock.DateIn(t, loc).Equal(clock.DateIn(now, loc)) {
		s += ", " + clock.FormatDateID(l)
	}
	return s
}

type confirmView struct {
	Participant auth.Participant
	Info        SessionInfo
	Phase       string
	Window      string // "08.00–09.30 WIB, 17 November 2026"
	Opens       string
	Error       string
	AttemptsAll bool
}

func (h *ParticipantHandler) loadInfo(w http.ResponseWriter, r *http.Request, id string) (auth.Participant, SessionInfo, bool) {
	p, _ := auth.CurrentParticipant(r.Context())
	info, err := GetSessionInfo(r.Context(), h.db, p, id)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, nil, "Ujian ini tidak ditemukan atau bukan untuk Anda.")
		return p, info, false
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant session", err)
		return p, info, false
	}
	return p, info, true
}

// windowLabel memformat jendela sesi: "08.00–09.30 WIB, 17 November 2026",
// atau dengan dua tanggal bila sesi melewati hari.
func windowLabel(start, end time.Time, loc *time.Location) string {
	a, b := start.In(loc), end.In(loc)
	if clock.DateIn(start, loc).Equal(clock.DateIn(end, loc)) {
		return a.Format("15.04") + "–" + b.Format("15.04") + " " + clock.ZoneLabel(loc) + ", " + clock.FormatDateID(a)
	}
	return clock.FormatDateID(a) + " " + a.Format("15.04") + " – " + clock.FormatDateID(b) + " " + b.Format("15.04") + " " + clock.ZoneLabel(loc)
}

func (h *ParticipantHandler) confirmViewFor(p auth.Participant, info SessionInfo, now time.Time) confirmView {
	loc := p.Location()
	return confirmView{Participant: p, Info: info, Phase: info.Phase(now), Window: windowLabel(info.StartsAt, info.EndsAt, loc),
		Opens: clockLabel(info.StartsAt, now, loc), AttemptsAll: info.MaxAttempts > 1}
}

func (h *ParticipantHandler) confirm(w http.ResponseWriter, r *http.Request) {
	p, info, ok := h.loadInfo(w, r, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	now := h.now()
	v := h.confirmViewFor(p, info, now)
	switch {
	case v.Phase == "berjalan":
		http.Redirect(w, r, "/u/a/"+info.Last.ID, http.StatusSeeOther)
		return
	case v.Phase == "selesai" && info.Last != nil:
		http.Redirect(w, r, "/u/a/"+info.Last.ID+"/selesai", http.StatusSeeOther)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/take_confirm", v)
}

// startMessage menerjemahkan error mulai menjadi pesan untuk peserta.
func startMessage(err error) (string, int) {
	switch {
	case errors.Is(err, ErrBadToken):
		return "Token salah. Tanyakan token kepada pengawas lalu ketik lagi.", http.StatusUnprocessableEntity
	case errors.Is(err, ErrNotOpen):
		return "Ujian belum dimulai. Tunggu sampai jam mulai.", http.StatusUnprocessableEntity
	case errors.Is(err, ErrSessionOver):
		return "Waktu sesi ujian sudah berakhir.", http.StatusUnprocessableEntity
	case errors.Is(err, ErrNoAttemptsLeft):
		return "Kesempatan mengerjakan ujian ini sudah habis.", http.StatusUnprocessableEntity
	case errors.Is(err, ErrOtherDevice):
		return "Ujian ini sedang dikerjakan di perangkat lain. Minta pengawas mengizinkan pindah perangkat.", http.StatusConflict
	}
	return "", http.StatusInternalServerError
}

func (h *ParticipantHandler) start(w http.ResponseWriter, r *http.Request) {
	p, info, ok := h.loadInfo(w, r, chi.URLParam(r, "session"))
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, nil, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	now := h.now()
	a, err := StartAttempt(r.Context(), h.db, p, StartRequest{SessionID: info.SessionID, Token: r.PostForm.Get("token"),
		DeviceID: h.ensureDevice(w, r), Now: now})
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, nil, "Ujian ini tidak ditemukan atau bukan untuk Anda.")
		return
	}
	if err != nil {
		msg, status := startMessage(err)
		if status == http.StatusInternalServerError {
			page.ServerError(h.render, h.logger, w, r, nil, "start attempt", err)
			return
		}
		h.logger.InfoContext(r.Context(), "attempt start rejected", "school_id", p.SchoolID, "session_id", info.SessionID, "reason", err.Error())
		v := h.confirmViewFor(p, info, now)
		v.Error = msg
		h.render.Render(w, r, status, "exam/take_confirm", v)
		return
	}
	h.logger.InfoContext(r.Context(), "attempt started", "school_id", p.SchoolID, "attempt_id", a.ID)
	http.Redirect(w, r, "/u/a/"+a.ID, http.StatusSeeOther)
}

// loadAttempt membaca attempt dari URL milik peserta yang masuk.
func (h *ParticipantHandler) loadAttempt(w http.ResponseWriter, r *http.Request) (auth.Participant, Attempt, bool) {
	p, _ := auth.CurrentParticipant(r.Context())
	a, err := GetAttempt(r.Context(), h.db, p, chi.URLParam(r, "attempt"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, nil, "Ujian ini tidak ditemukan atau bukan untuk Anda.")
		return p, a, false
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "participant attempt", err)
		return p, a, false
	}
	return p, a, true
}

// takeState adalah keadaan awal halaman ujian (JSON di halaman).
type takeState struct {
	IDs      []string               `json:"ids"`
	Answers  map[string]AnswerState `json:"answers"`
	Pos      int                    `json:"pos"`
	Deadline int64                  `json:"deadline"` // ms Unix
	Now      int64                  `json:"now"`
	Back     bool                   `json:"back"`
	Sound    bool                   `json:"sound"`
	Key      string                 `json:"key"`   // kunci penyimpanan preferensi tampilan
	Token    string                 `json:"token"` // token attempt untuk simpan (T139)
}

type takeView struct {
	Participant auth.Participant
	Info        SessionInfo
	Attempt     Attempt
	Delivery    Delivery
	State       takeState
}

func (h *ParticipantHandler) take(w http.ResponseWriter, r *http.Request) {
	p, a, ok := h.loadAttempt(w, r)
	if !ok {
		return
	}
	now := h.now()
	if a.Running() && !now.Before(a.DeadlineAt.Add(SaveGrace)) {
		// Waktu habis saat perangkat mati/tertutup: jawaban tersimpan dikumpulkan.
		var err error
		if a, err = SubmitAttempt(r.Context(), h.db, p, a, SubmitByTime, now); err != nil {
			page.ServerError(h.render, h.logger, w, r, nil, "submit expired attempt", err)
			return
		}
	}
	if !a.Running() {
		http.Redirect(w, r, "/u/a/"+a.ID+"/selesai", http.StatusSeeOther)
		return
	}
	if a.DeviceID != h.deviceID(r) {
		v := confirmView{Participant: p, Error: "Ujian ini sedang dikerjakan di perangkat lain. Minta pengawas mengizinkan pindah perangkat."}
		h.render.Render(w, r, http.StatusConflict, "exam/take_device", v)
		return
	}
	info, err := GetSessionInfo(r.Context(), h.db, p, a.SessionID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "attempt session", err)
		return
	}
	e, err := GetExam(r.Context(), h.db, p.SchoolID, a.ExamID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "attempt exam", err)
		return
	}
	d, err := LoadDelivery(r.Context(), h.db, e, a, func(id string) string { return "/u/a/" + a.ID + "/gambar/" + id })
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "attempt delivery", err)
		return
	}
	answers, err := LoadAnswers(r.Context(), h.db, p.SchoolID, a.ID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "attempt answers", err)
		return
	}
	token, err := h.tokens.TokenFor(a)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "attempt token", err)
		return
	}
	owner := p.StudentID + p.ApplicantID
	v := takeView{Participant: p, Info: info, Attempt: a, Delivery: d, State: takeState{IDs: a.QuestionOrder, Answers: answers,
		Pos: a.CurrentPosition, Deadline: a.DeadlineAt.UnixMilli(), Now: now.UnixMilli(), Back: e.AllowBack, Sound: info.AlertSound(),
		Key: "akra-ujian-" + owner, Token: token}}
	// Halaman berisi soal: jangan disimpan cache bersama atau riwayat.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if fr, ok := h.render.(fragmentRenderer); ok {
		fr.RenderFragment(w, r, http.StatusOK, "exam/take", "take", v)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/take", v)
}

// Batas body simpan: 200 perubahan, sebagian besar kecil.
const maxSaveBody = 512 << 10

type saveRequest struct {
	Changes []AnswerChange `json:"changes"`
	Pos     *int           `json:"pos"`
}

type saveResponse struct {
	OK       bool   `json:"ok"`
	Seq      int64  `json:"seq,omitempty"` // seq tertinggi yang sudah aman di server
	Closed   bool   `json:"closed,omitempty"`
	Redirect string `json:"redirect,omitempty"`
	Now      int64  `json:"now"`
	Deadline int64  `json:"deadline,omitempty"`
}

// save menyimpan perubahan jawaban (T139). Hanya memverifikasi token
// attempt (CPU) lalu menjalankan satu pernyataan SQL; tidak membaca sesi
// peserta. Attempt yang sudah dikumpulkan, dipindah perangkat, atau lewat
// tenggat + tenggang dijawab 409.
func (h *ParticipantHandler) save(w http.ResponseWriter, r *http.Request) {
	now := h.now()
	w.Header().Set("Cache-Control", "no-store")
	id := chi.URLParam(r, "attempt")
	c, err := h.tokens.Verify(r.Header.Get(AttemptTokenHeader), now)
	if err != nil || c.AttemptID != id {
		// Token kedaluwarsa/salah: halaman dimuat ulang untuk token baru
		// (lewat sesi peserta); jawaban tetap di antrian perangkat.
		writeJSON(w, http.StatusUnauthorized, saveResponse{Redirect: "/u/a/" + id, Now: now.UnixMilli()})
		return
	}
	var req saveRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSaveBody))
	if err := dec.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, saveResponse{Now: now.UnixMilli()})
		return
	}
	pos := -1
	if req.Pos != nil {
		pos = *req.Pos
	}
	res, err := SaveAnswers(r.Context(), h.db, c.SchoolID, c.AttemptID, c.DeviceID, req.Changes, pos, now)
	switch {
	case errors.Is(err, ErrAttemptClosed):
		writeJSON(w, http.StatusConflict, saveResponse{Closed: true, Redirect: "/u/a/" + id, Now: now.UnixMilli()})
	case errors.Is(err, ErrBadAnswer):
		writeJSON(w, http.StatusBadRequest, saveResponse{Now: now.UnixMilli()})
	case err != nil:
		h.logger.ErrorContext(r.Context(), "save answers", "attempt_id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, saveResponse{Now: now.UnixMilli()})
	default:
		writeJSON(w, http.StatusOK, saveResponse{OK: true, Seq: res.Seq, Now: now.UnixMilli(), Deadline: res.Deadline.UnixMilli()})
	}
}

func (h *ParticipantHandler) submit(w http.ResponseWriter, r *http.Request) {
	p, a, ok := h.loadAttempt(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		page.BadRequest(h.render, w, r, nil, "Form tidak valid", "Isian form tidak bisa dibaca. Muat ulang halaman lalu coba lagi.")
		return
	}
	if a.Running() && a.DeviceID != h.deviceID(r) {
		v := confirmView{Participant: p, Error: "Ujian ini sedang dikerjakan di perangkat lain. Minta pengawas mengizinkan pindah perangkat."}
		h.render.Render(w, r, http.StatusConflict, "exam/take_device", v)
		return
	}
	reason := SubmitByParticipant
	if r.PostForm.Get("alasan") == SubmitByTime {
		reason = SubmitByTime
	}
	a, err := SubmitAttempt(r.Context(), h.db, p, a, reason, h.now())
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "submit attempt", err)
		return
	}
	h.logger.InfoContext(r.Context(), "attempt submitted", "school_id", p.SchoolID, "attempt_id", a.ID, "reason", a.SubmitReason)
	http.Redirect(w, r, "/u/a/"+a.ID+"/selesai", http.StatusSeeOther)
}

type doneView struct {
	Participant auth.Participant
	Info        SessionInfo
	Attempt     Attempt
	Received    string
	ByTime      bool
	CanRetry    bool
}

func (h *ParticipantHandler) done(w http.ResponseWriter, r *http.Request) {
	p, a, ok := h.loadAttempt(w, r)
	if !ok {
		return
	}
	if a.Running() {
		http.Redirect(w, r, "/u/a/"+a.ID, http.StatusSeeOther)
		return
	}
	info, err := GetSessionInfo(r.Context(), h.db, p, a.SessionID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, nil, "attempt session", err)
		return
	}
	now := h.now()
	v := doneView{Participant: p, Info: info, Attempt: a, ByTime: a.SubmitReason == SubmitByTime,
		CanRetry: !p.ReadOnly && info.Phase(now) == "buka"}
	if a.SubmittedAt != nil {
		v.Received = clockLabel(*a.SubmittedAt, now, p.Location())
	}
	w.Header().Set("Cache-Control", "private, no-store")
	h.render.Render(w, r, http.StatusOK, "exam/take_done", v)
}

// image mengalihkan ke URL bertanda tangan gambar soal yang berlaku sampai
// tenggat attempt. Path ini tetap (tanpa tanda tangan) agar bisa menjadi
// kunci cache perangkat (T139).
func (h *ParticipantHandler) image(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.CurrentParticipant(r.Context())
	a, err := GetAttempt(r.Context(), h.db, p, chi.URLParam(r, "attempt"))
	now := h.now()
	if err == nil && (!a.Running() || h.store == nil) {
		err = ErrNotFound
	}
	var asset Asset
	if err == nil {
		asset, err = GetAsset(r.Context(), h.db, p.SchoolID, chi.URLParam(r, "asset"))
	}
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.logger.ErrorContext(r.Context(), "attempt image", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	ttl := a.DeadlineAt.Add(SaveGrace).Sub(now)
	if ttl < time.Minute {
		ttl = time.Minute
	}
	u, err := h.store.Presign(r.Context(), asset.StorageKey, ttl, storage.PresignOptions{ContentDisposition: "inline", ContentType: asset.ContentType})
	if err != nil {
		h.logger.ErrorContext(r.Context(), "presign attempt image", "error", err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=60")
	http.Redirect(w, r, u, http.StatusFound)
}

// homeExam adalah satu ujian di beranda peserta.
type homeExam struct {
	ID, Name, Subject, Date, Time string
	Status, Tone, Action, Href    string
}

// homeExams menyusun daftar ujian beranda dari sesi peserta.
func homeExams(sessions []SessionInfo, now time.Time, loc *time.Location, readOnly bool) []homeExam {
	var out []homeExam
	for _, s := range sessions {
		a, b := s.StartsAt.In(loc), s.EndsAt.In(loc)
		e := homeExam{ID: s.SessionID, Name: s.ExamName, Subject: s.Subject, Date: dayNames[a.Weekday()] + ", " + clock.FormatDateID(a),
			Time: a.Format("15.04") + "–" + b.Format("15.04") + " " + clock.ZoneLabel(loc), Href: "/u/ujian/" + s.SessionID}
		if !clock.DateIn(s.StartsAt, loc).Equal(clock.DateIn(s.EndsAt, loc)) {
			e.Date, e.Time = "sampai "+dayNames[b.Weekday()]+", "+clock.FormatDateID(b), b.Format("15.04")+" "+clock.ZoneLabel(loc)
		}
		switch s.Phase(now) {
		case "berjalan":
			e.Status, e.Tone, e.Action = "Sedang dikerjakan", "warning", "Lanjutkan"
		case "belum":
			e.Status, e.Tone = "Belum dimulai", "info"
		case "buka":
			e.Status, e.Tone, e.Action = "Bisa dikerjakan", "success", "Kerjakan"
			if s.Attempts > 0 {
				e.Action = "Kerjakan lagi"
			}
		case "selesai":
			e.Status = "Sudah dikumpulkan"
			if s.Last != nil {
				e.Href = "/u/a/" + s.Last.ID + "/selesai"
			}
		case "terlewat":
			e.Status, e.Tone = "Tidak dikerjakan", "danger"
		default:
			continue
		}
		if readOnly {
			e.Action = ""
		}
		out = append(out, e)
	}
	return out
}
