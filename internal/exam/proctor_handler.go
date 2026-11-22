package exam

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
)

// proctorRoutes mendaftarkan layar pengawas (T143). Peran: admin_tu,
// kepala_sekolah, guru; hak per sesi dari ProctorLevel (bukan pengawas
// sesi → 404).
func (h *Handler) proctorRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah, auth.RoleGuru))
		r.Use(h.withActor)
		r.Get("/sesi-saya", h.mySessions)
		r.Get("/sesi/{sid}/awasi", h.proctorPage)
		r.Get("/sesi/{sid}/awasi/data", h.proctorData)
		r.Get("/sesi/{sid}/token", h.proctorToken)
		r.Get("/sesi/{sid}/token.json", h.proctorTokenJSON)
		r.Get("/sesi/{sid}/berita-acara.pdf", h.proctorMinutesPDF)
		r.Post("/sesi/{sid}/awasi/aksi", h.proctorSessionAction)
		r.Post("/sesi/{sid}/awasi/peserta/{pid}", h.proctorParticipantAction)
		r.Post("/sesi/{sid}/berita-acara", h.proctorSign)
	})
}

// withActor memuat Actor ke konteks tanpa syarat akses; hak dicek per sesi.
func (h *Handler) withActor(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := ActorFrom(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		id, _ := auth.CurrentUser(r.Context())
		a, err := LoadActor(r.Context(), h.db, id)
		if err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "load exam actor", err)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), actorKey{}, a)))
	})
}

func proctorPath(sid string) string { return "/ujian/sesi/" + sid + "/awasi" }

// proctorSession membaca sesi dan hak pengguna; menulis 404 bila tidak
// berhak atau sesi tidak ada.
func (h *Handler) proctorSession(w http.ResponseWriter, r *http.Request) (StaffActor, ProctorSession, Level, bool) {
	id, sa := h.staff(r)
	now := time.Now()
	sid := chi.URLParam(r, "sid")
	ps, err := GetProctorSession(r.Context(), h.db, sa.SchoolID, sid, sa.UserID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Sesi ujian tidak ditemukan.")
		return sa, ps, None, false
	}
	if err != nil {
		h.fail(w, r, "proctor session", err)
		return sa, ps, None, false
	}
	lv, err := ProctorLevel(r.Context(), h.db, sa.Actor, sa.SchoolID, sid, now)
	if err != nil {
		h.fail(w, r, "proctor level", err)
		return sa, ps, None, false
	}
	if lv == None {
		h.logger.WarnContext(r.Context(), "proctor access denied", "user_id", sa.UserID, "session_id", sid)
		page.NotFound(h.render, w, r, id, "Sesi ujian tidak ditemukan.")
		return sa, ps, None, false
	}
	return sa, ps, lv, true
}

// proctorRow adalah satu peserta untuk tampilan.
type proctorRow struct {
	MonitorRow
	Left    string
	Seen    string
	Filters string // status untuk filter: belum, mengerjakan, offline, perhatian, selesai
}

type proctorLive struct {
	Rows      []proctorRow
	Counts    map[string]int
	Total     int
	Phase     string
	Remaining string
	UpdatedAt int64
	CanWrite  bool
	Paused    bool
}

type proctorView struct {
	User     auth.Identity
	S        ProctorSession
	When     string
	Zone     string
	Live     proctorLive
	Token    SessionToken
	CanWrite bool
	Flash    string
	NewCode  *newCodeView
	Minutes  MinutesData
	Error    string
	Ended    bool
}

type newCodeView struct{ Name, Code string }

func clockLeft(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s <= 0 {
		return "0:00"
	}
	h, m, sec := s/3600, (s%3600)/60, s%60
	if h > 0 {
		return strconv.Itoa(h) + ":" + pad2(m) + ":" + pad2(sec)
	}
	return strconv.Itoa(m) + ":" + pad2(sec)
}

func pad2(n int) string {
	if n < 10 {
		return "0" + strconv.Itoa(n)
	}
	return strconv.Itoa(n)
}

// liveData memuat ringkasan dan daftar peserta, memutar token, dan
// mencatat pengawas sedang membuka layar.
func (h *Handler) liveData(ctx context.Context, sa StaffActor, ps ProctorSession, lv Level, now time.Time) (proctorLive, SessionToken, error) {
	mon, err := LoadMonitor(ctx, h.db, sa.SchoolID, ps.ID, now)
	if err != nil {
		return proctorLive{}, SessionToken{}, err
	}
	var tok SessionToken
	if lv >= Write && ps.Phase(now) != "selesai" {
		if tok, err = EnsureToken(ctx, h.db, sa.SchoolID, ps.ID, now); err != nil {
			return proctorLive{}, SessionToken{}, err
		}
	}
	if ps.Proctored && lv >= Write {
		if err := MarkProctorSeen(ctx, h.db, sa.SchoolID, ps.ID, now); err != nil {
			return proctorLive{}, SessionToken{}, err
		}
	}
	live := proctorLive{Counts: mon.Counts, Total: len(mon.Rows), Phase: ps.Phase(now), UpdatedAt: now.UnixMilli(),
		CanWrite: lv >= Write, Paused: ps.PausedAt != nil}
	if live.Phase == "berlangsung" {
		live.Remaining = clockLeft(ps.EndsAtEffective().Sub(now))
	}
	for _, r := range mon.Rows {
		pr := proctorRow{MonitorRow: r, Filters: r.State}
		if r.NeedsAttention() {
			pr.Filters += " perhatian"
		}
		if r.State == MonWorking || r.State == MonOffline {
			pr.Left = clockLeft(r.Left)
			if r.Paused {
				pr.Left = "dijeda"
			}
		}
		if r.State == MonOffline {
			pr.Seen = awayLabel(now.Sub(r.LastSeen)) + " lalu"
		}
		live.Rows = append(live.Rows, pr)
	}
	return live, tok, nil
}

func (h *Handler) proctorView(w http.ResponseWriter, r *http.Request) (proctorView, bool) {
	sa, ps, lv, ok := h.proctorSession(w, r)
	if !ok {
		return proctorView{}, false
	}
	id, _ := auth.CurrentUser(r.Context())
	now := time.Now()
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return proctorView{}, false
	}
	live, tok, err := h.liveData(r.Context(), sa, ps, lv, now)
	if err != nil {
		h.fail(w, r, "proctor live", err)
		return proctorView{}, false
	}
	v := proctorView{User: id, S: ps, When: sessionWhen(ps.Session, lh.Loc), Zone: clock.ZoneLabel(lh.Loc), Live: live, Token: tok,
		CanWrite: lv >= Write, Flash: auth.PopFlash(r.Context(), h.sessions), Ended: live.Phase == "selesai"}
	if v.Ended {
		if v.Minutes, err = LoadMinutes(r.Context(), h.db, sa.SchoolID, ps.ID, lh.Loc); err != nil {
			h.fail(w, r, "load minutes", err)
			return proctorView{}, false
		}
	}
	return v, true
}

func (h *Handler) proctorPage(w http.ResponseWriter, r *http.Request) {
	v, ok := h.proctorView(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	h.render.Render(w, r, http.StatusOK, "exam/proctor", v)
}

// proctorData mengembalikan potongan ringkasan dan daftar peserta untuk
// pembaruan tiap 10 detik.
func (h *Handler) proctorData(w http.ResponseWriter, r *http.Request) {
	sa, ps, lv, ok := h.proctorSession(w, r)
	if !ok {
		return
	}
	live, _, err := h.liveData(r.Context(), sa, ps, lv, time.Now())
	if err != nil {
		h.fail(w, r, "proctor live", err)
		return
	}
	w.Header().Set("Cache-Control", "private, no-store")
	fr, ok := h.render.(fragmentRenderer)
	if !ok {
		http.Error(w, "fragment renderer unavailable", http.StatusInternalServerError)
		return
	}
	fr.RenderFragment(w, r, http.StatusOK, "exam/proctor", "proctor_live", proctorView{S: ps, Live: live, CanWrite: lv >= Write})
}

type tokenView struct {
	User  auth.Identity
	S     ProctorSession
	Token SessionToken
}

// proctorToken menampilkan token besar untuk ditunjukkan ke kelas atau
// proyektor.
func (h *Handler) proctorToken(w http.ResponseWriter, r *http.Request) {
	sa, ps, lv, ok := h.proctorSession(w, r)
	if !ok {
		return
	}
	if lv < Write {
		id, _ := auth.CurrentUser(r.Context())
		page.Forbidden(h.render, w, r, id, "Token hanya untuk pengawas sesi selama jendela pengawasan.")
		return
	}
	tok, err := EnsureToken(r.Context(), h.db, sa.SchoolID, ps.ID, time.Now())
	if err != nil {
		h.fail(w, r, "session token", err)
		return
	}
	id, _ := auth.CurrentUser(r.Context())
	w.Header().Set("Cache-Control", "private, no-store")
	h.render.Render(w, r, http.StatusOK, "exam/proctor_token_fullscreen", tokenView{User: id, S: ps, Token: tok})
}

func (h *Handler) proctorTokenJSON(w http.ResponseWriter, r *http.Request) {
	sa, ps, lv, ok := h.proctorSession(w, r)
	if !ok {
		return
	}
	if lv < Write {
		http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
		return
	}
	now := time.Now()
	tok, err := EnsureToken(r.Context(), h.db, sa.SchoolID, ps.ID, now)
	if err != nil {
		h.fail(w, r, "session token", err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{"required": tok.Required, "token": tok.Token, "next": tok.NextAt.UnixMilli(),
		"now": now.UnixMilli()})
}

// proctorWrite memeriksa hak tulis aksi pengawas dan membaca form.
func (h *Handler) proctorWrite(w http.ResponseWriter, r *http.Request) (StaffActor, ProctorSession, bool) {
	if !h.parseForm(w, r, 8<<10) {
		return StaffActor{}, ProctorSession{}, false
	}
	sa, ps, lv, ok := h.proctorSession(w, r)
	if !ok {
		return sa, ps, false
	}
	if lv < Write {
		id, _ := auth.CurrentUser(r.Context())
		page.Forbidden(h.render, w, r, id, "Aksi pengawas hanya bisa dilakukan pengawas sesi selama jendela pengawasan (30 menit sebelum sampai 2 jam sesudah sesi).")
		return sa, ps, false
	}
	return sa, ps, true
}

// actionMessage menerjemahkan error aksi pengawas menjadi pesan.
func actionMessage(err error) string {
	switch {
	case errors.Is(err, ErrAttemptClosed):
		return "Peserta ini sudah selesai atau waktunya habis."
	case errors.Is(err, ErrBadExtra):
		return "Waktu tambahan 1–60 menit, total paling banyak 240 menit."
	case errors.Is(err, ErrBadNote):
		return "Isi catatan (paling banyak 500 karakter)."
	case errors.Is(err, ErrSessionOver):
		return "Sesi sudah berakhir."
	case errors.Is(err, ErrNotFound):
		return "Peserta belum mulai mengerjakan atau tidak ditemukan."
	}
	return ""
}

// proctorSessionAction menjalankan aksi sesi: jeda/lanjut semua, tambah
// waktu semua, tutup sesi, token, catatan.
func (h *Handler) proctorSessionAction(w http.ResponseWriter, r *http.Request) {
	sa, ps, ok := h.proctorWrite(w, r)
	if !ok {
		return
	}
	ctx, now := r.Context(), time.Now()
	f := r.PostForm
	var msg string
	var err error
	switch f.Get("aksi") {
	case "jeda":
		var n int
		n, err = PauseSession(ctx, h.db, sa, ps.ID, now)
		msg = "Sesi dijeda. " + strconv.Itoa(n) + " peserta berhenti; layar mereka terkunci."
	case "lanjut":
		var n int
		n, err = ResumeSession(ctx, h.db, sa, ps.ID, now)
		msg = "Sesi dilanjutkan. Lama jeda ditambahkan ke waktu " + strconv.Itoa(n) + " peserta."
	case "waktu":
		mins, _ := strconv.Atoi(f.Get("menit"))
		err = AddSessionTime(ctx, h.db, sa, ps.ID, mins, now)
		msg = "Waktu semua peserta ditambah " + strconv.Itoa(mins) + " menit."
	case "tutup":
		var n int
		n, err = CloseSession(ctx, h.db, sa, ps.ID, now)
		msg = "Sesi ditutup. " + strconv.Itoa(n) + " peserta yang masih mengerjakan dikumpulkan."
	case "token_on", "token_off":
		err = SetTokenRequired(ctx, h.db, sa, ps.ID, f.Get("aksi") == "token_on", now)
		msg = map[bool]string{true: "Token diwajibkan. Bacakan token kepada peserta.", false: "Token tidak lagi diwajibkan."}[f.Get("aksi") == "token_on"]
	case "catat":
		err = AddNote(ctx, h.db, sa, ps.ID, "", f.Get("catatan"))
		msg = "Catatan disimpan untuk berita acara."
	default:
		err = ErrNotFound
	}
	if err == nil && f.Get("aksi") != "catat" && strings.TrimSpace(f.Get("alasan")) != "" {
		err = AddNote(ctx, h.db, sa, ps.ID, "", sessionActionLabel(f.Get("aksi"))+": "+f.Get("alasan"))
	}
	h.proctorDone(w, r, ps, msg, err)
}

func sessionActionLabel(a string) string {
	return map[string]string{"jeda": "Jeda semua", "lanjut": "Lanjutkan semua", "waktu": "Tambah waktu semua", "tutup": "Tutup sesi",
		"token_on": "Token diwajibkan", "token_off": "Token dilepas"}[a]
}

func participantActionLabel(a string) string {
	return map[string]string{"pindah": "Izin pindah perangkat", "buka": "Buka kunci", "reset": "Reset login", "waktu": "Tambah waktu",
		"jeda": "Jeda", "lanjut": "Lanjutkan", "kumpul": "Kumpulkan paksa"}[a]
}

// runningAttempt mengembalikan attempt berjalan peserta di sesi.
func (h *Handler) runningAttempt(ctx context.Context, schoolID, sessionID, participantID string) (string, error) {
	var id string
	err := h.db.QueryRow(ctx, `SELECT t.id FROM exam_attempts t JOIN exam_session_participants p ON p.id = t.participant_id
		WHERE t.school_id = $1 AND p.session_id = $2 AND p.id = $3 AND t.status = 'berjalan'`, schoolID, sessionID, participantID).Scan(&id)
	if err != nil {
		return "", ErrNotFound
	}
	return id, nil
}

// proctorParticipantAction menjalankan aksi untuk satu peserta.
func (h *Handler) proctorParticipantAction(w http.ResponseWriter, r *http.Request) {
	sa, ps, ok := h.proctorWrite(w, r)
	if !ok {
		return
	}
	ctx, now := r.Context(), time.Now()
	pid := chi.URLParam(r, "pid")
	f := r.PostForm
	var name string
	if err := h.db.QueryRow(ctx, `SELECT coalesce(s.full_name, a.full_name, '') FROM exam_session_participants p
		LEFT JOIN students s ON s.id = p.student_id LEFT JOIN applicants a ON a.id = p.applicant_id
		WHERE p.school_id = $1 AND p.id = $2 AND p.session_id = $3`, sa.SchoolID, pid, ps.ID).Scan(&name); err != nil || !isUUIDish(pid) {
		h.proctorDone(w, r, ps, "", ErrNotFound)
		return
	}
	attempt := func() (string, error) { return h.runningAttempt(ctx, sa.SchoolID, ps.ID, pid) }
	var msg string
	var err error
	aksi := f.Get("aksi")
	switch aksi {
	case "pindah":
		err = AllowDeviceMove(ctx, h.db, sa, pid)
		msg = name + " boleh masuk dari perangkat lain. Perangkat lama tidak bisa menyimpan lagi."
	case "buka":
		var id string
		if id, err = attempt(); err == nil {
			err = UnlockAttempt(ctx, h.db, sa, id, now)
		}
		msg = "Kunci " + name + " dibuka."
	case "reset":
		var code string
		code, err = ResetLogin(ctx, h.db, h.issuer, sa, pid, now)
		if err == nil {
			if r := strings.TrimSpace(f.Get("alasan")); r != "" {
				_ = AddNote(ctx, h.db, sa, ps.ID, pid, participantActionLabel(aksi)+": "+r)
			}
			h.logger.InfoContext(ctx, "participant login reset", "school_id", sa.SchoolID, "session_id", ps.ID, "participant_id", pid)
			v, ok := h.proctorView(w, r)
			if !ok {
				return
			}
			v.NewCode = &newCodeView{Name: name, Code: code}
			w.Header().Set("Cache-Control", "private, no-store")
			h.render.Render(w, r, http.StatusOK, "exam/proctor", v)
			return
		}
	case "waktu":
		mins, _ := strconv.Atoi(f.Get("menit"))
		err = AddParticipantTime(ctx, h.db, sa, pid, mins, now)
		msg = "Waktu " + name + " ditambah " + strconv.Itoa(mins) + " menit (tetap dibatasi akhir sesi)."
	case "jeda":
		var id string
		if id, err = attempt(); err == nil {
			err = PauseAttempt(ctx, h.db, sa, id, now)
		}
		msg = name + " dijeda; layarnya terkunci dan waktunya berhenti."
	case "lanjut":
		var id string
		if id, err = attempt(); err == nil {
			err = ResumeAttempt(ctx, h.db, sa, id, now)
		}
		msg = name + " dilanjutkan."
	case "kumpul":
		var id string
		if id, err = attempt(); err == nil {
			err = ForceSubmit(ctx, h.db, sa, id, now)
		}
		msg = "Jawaban " + name + " dikumpulkan."
	case "catat":
		err = AddNote(ctx, h.db, sa, ps.ID, pid, f.Get("catatan"))
		msg = "Catatan untuk " + name + " disimpan."
	default:
		err = ErrNotFound
	}
	if err == nil && aksi != "catat" && strings.TrimSpace(f.Get("alasan")) != "" {
		err = AddNote(ctx, h.db, sa, ps.ID, pid, participantActionLabel(aksi)+": "+f.Get("alasan"))
	}
	h.proctorDone(w, r, ps, msg, err)
}

func isUUIDish(s string) bool { return len(s) == 36 && strings.Count(s, "-") == 4 }

// proctorDone mengalihkan kembali ke layar pengawas dengan pesan.
func (h *Handler) proctorDone(w http.ResponseWriter, r *http.Request, ps ProctorSession, msg string, err error) {
	if err != nil {
		m := actionMessage(err)
		if m == "" {
			h.fail(w, r, "proctor action", err)
			return
		}
		msg = m
	}
	h.flash(r, msg)
	http.Redirect(w, r, proctorPath(ps.ID), http.StatusSeeOther)
}

// proctorSign menandatangani berita acara.
func (h *Handler) proctorSign(w http.ResponseWriter, r *http.Request) {
	sa, ps, ok := h.proctorWrite(w, r)
	if !ok {
		return
	}
	err := SignMinutes(r.Context(), h.db, sa, ps.ID, r.PostForm.Get("catatan"), r.PostForm.Get("sandi"), time.Now())
	switch {
	case errors.Is(err, ErrBadPassword), errors.Is(err, ErrSessionNotEnded), errors.Is(err, ErrBadNote):
		v, ok := h.proctorView(w, r)
		if !ok {
			return
		}
		v.Error = map[bool]string{true: "Sandi salah. Ketik sandi akun Anda untuk menandatangani.",
			false: "Berita acara bisa ditandatangani setelah sesi berakhir atau ditutup."}[errors.Is(err, ErrBadPassword)]
		if errors.Is(err, ErrBadNote) {
			v.Error = "Catatan paling banyak 4.000 karakter."
		}
		v.Minutes.Notes = r.PostForm.Get("catatan")
		h.render.Render(w, r, http.StatusUnprocessableEntity, "exam/proctor", v)
		return
	case err != nil:
		h.fail(w, r, "sign minutes", err)
		return
	}
	h.flash(r, "Berita acara ditandatangani.")
	http.Redirect(w, r, proctorPath(ps.ID), http.StatusSeeOther)
}

func (h *Handler) proctorMinutesPDF(w http.ResponseWriter, r *http.Request) {
	sa, ps, _, ok := h.proctorSession(w, r)
	if !ok {
		return
	}
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return
	}
	docs, err := LoadSessionDocs(r.Context(), h.db, sa.SchoolID, ps.ExamID, "", "", ps.ID, lh.Loc)
	if err != nil {
		h.fail(w, r, "session docs", err)
		return
	}
	att, ok := h.minutesFor(w, r, docs, lh)
	if !ok {
		return
	}
	h.writePDF(w, r, "berita-acara-"+ps.ExamName, func(b *bytes.Buffer) error { return WriteMinutesPDF(b, lh, docs, att, time.Now(), true) })
}

type mySessionsView struct {
	User     auth.Identity
	Sessions []MySessionCard
	Zone     string
}

// MySessionCard adalah kartu sesi di beranda pengawas.
type MySessionCard struct {
	MySession
	Time, Status string
}

// DayBounds mengembalikan awal hari ini dan awal besok di zona loc.
func DayBounds(now time.Time, loc *time.Location) (time.Time, time.Time) {
	y, m, d := now.In(loc).Date()
	from := time.Date(y, m, d, 0, 0, 0, 0, loc)
	return from, from.AddDate(0, 0, 1)
}

// MySessionCards menyusun kartu "Sesi saya hari ini" untuk beranda.
func MySessionCards(ss []MySession, loc *time.Location, now time.Time) []MySessionCard {
	out := make([]MySessionCard, 0, len(ss))
	for _, s := range ss {
		c := MySessionCard{MySession: s, Time: s.StartsAt.In(loc).Format("15.04") + "–" + s.EndsAt.In(loc).Format("15.04")}
		switch {
		case s.ClosedAt != nil || !now.Before(s.EndsAt):
			c.Status = "Selesai"
		case now.Before(s.StartsAt):
			c.Status = "Belum mulai"
		default:
			c.Status = "Berlangsung"
		}
		out = append(out, c)
	}
	return out
}

// mySessions menampilkan sesi yang diawasi pengguna hari ini.
func (h *Handler) mySessions(w http.ResponseWriter, r *http.Request) {
	id, sa := h.staff(r)
	lh, err := LoadLetterhead(r.Context(), h.db, sa.SchoolID)
	if err != nil {
		h.fail(w, r, "letterhead", err)
		return
	}
	now := time.Now()
	from, to := DayBounds(now, lh.Loc)
	ss, err := MyProctorSessions(r.Context(), h.db, sa.SchoolID, sa.UserID, from, to)
	if err != nil {
		h.fail(w, r, "my sessions", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "exam/my_sessions", mySessionsView{User: id, Sessions: MySessionCards(ss, lh.Loc, now),
		Zone: clock.ZoneLabel(lh.Loc)})
}
