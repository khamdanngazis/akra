package exam

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"akra/internal/duty"
	"akra/internal/testutil"
)

// controlEnv: sesi proctorEnv dijadikan sesi agenda (ruang A, berpengawas)
// ditambah sesi kedua ujian yang sama di ruang B tanpa pengawas.
type controlEnv struct {
	*proctorEnv
	roomB, sessB string
	page         string
}

func newControlEnv(t *testing.T) *controlEnv {
	t.Helper()
	e := &controlEnv{proctorEnv: newProctorEnv(t)}
	ctx := context.Background()
	a := e.a
	if _, err := e.tx.Exec(ctx, `UPDATE exam_sessions SET event_id = $2, room_id = $3, cell_id = $4, shift = 1 WHERE id = $1`,
		e.sess.ID, a.ExamEventID, a.ExamRoomID, a.ExamCellID); err != nil {
		t.Fatal(err)
	}
	if err := e.tx.QueryRow(ctx, `INSERT INTO exam_rooms (school_id, name, kind, capacity) VALUES ($1, 'Ruang B', 'kelas', 30) RETURNING id`,
		a.ID).Scan(&e.roomB); err != nil {
		t.Fatal(err)
	}
	if err := e.tx.QueryRow(ctx, `INSERT INTO exam_sessions (school_id, exam_id, event_id, room_id, cell_id, shift, label, location, starts_at, ends_at, created_by)
		SELECT school_id, exam_id, event_id, $2, cell_id, 1, 'Ruang B', 'di_ruang', starts_at, ends_at, created_by FROM exam_sessions WHERE id = $1
		RETURNING id`, e.sess.ID, e.roomB).Scan(&e.sessB); err != nil {
		t.Fatal(err)
	}
	e.page = "/ujian/agenda/" + a.ExamEventID + "/kendali"
	return e
}

func (e *controlEnv) units(t *testing.T, now time.Time) map[string]ControlUnit {
	t.Helper()
	from, to := DayBounds(now, time.Local)
	ss, err := LoadControlSessions(context.Background(), e.tx, e.a.ID, e.a.ExamEventID, from.Add(-24*time.Hour), to.Add(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]ControlUnit{}
	for _, u := range BuildControlUnits(ss, now) {
		out[u.RoomID] = u
	}
	return out
}

func TestControlAccess(t *testing.T) {
	e := newControlEnv(t)
	coordinator := testutil.User{ID: e.teacher2, Role: "guru"}

	rec := e.staffDo(http.MethodGet, e.page, e.a.Admin, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Ruang B") || !strings.Contains(rec.Body.String(), "Aksi untuk beberapa ruang") {
		t.Fatalf("admin control = %d", rec.Code)
	}
	rec = e.staffDo(http.MethodGet, e.page, e.a.Principal, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "Aksi untuk beberapa ruang") || !strings.Contains(rec.Body.String(), "hanya bisa melihat") {
		t.Errorf("principal control = %d (must be read only)", rec.Code)
	}
	if rec := e.staffDo(http.MethodGet, e.page+"/data", e.a.Principal, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "control-card") {
		t.Errorf("principal live data = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodPost, e.page+"/aksi", e.a.Principal, url.Values{"aksi": {"jeda"}, "target": {"semua"}}); rec.Code != http.StatusForbidden {
		t.Errorf("principal action = %d, want 403", rec.Code)
	}
	// Guru biasa (termasuk pengawas sesi): 403. Koordinator: aksi.
	if rec := e.staffDo(http.MethodGet, e.page, coordinator, nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher control = %d, want 403", rec.Code)
	}
	if rec := e.staffDo(http.MethodGet, e.page, testutil.User{ID: e.a.Teacher.ID, Role: "guru"}, nil); rec.Code != http.StatusForbidden {
		t.Errorf("proctor control = %d, want 403", rec.Code)
	}
	if _, err := duty.Assign(context.Background(), e.tx, duty.Actor{SchoolID: e.a.ID, UserID: e.a.Admin.ID}, e.a.YearID, e.teacher2, duty.ExamCoordinator); err != nil {
		t.Fatal(err)
	}
	if rec := e.staffDo(http.MethodGet, e.page, coordinator, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Aksi untuk beberapa ruang") {
		t.Errorf("coordinator control = %d", rec.Code)
	}
	// Agenda sekolah lain: 404.
	if rec := e.staffDo(http.MethodGet, "/ujian/agenda/"+e.b.ExamEventID+"/kendali", e.a.Admin, nil); rec.Code != http.StatusNotFound {
		t.Errorf("other school event = %d, want 404", rec.Code)
	}
	// Sesi sekolah lain sebagai sasaran aksi diabaikan.
	rec = e.staffDo(http.MethodPost, e.page+"/aksi", e.a.Admin, url.Values{"aksi": {"pesan"}, "pesan": {"x"}, "sid": {e.b.ExamSessionID}})
	var n int
	e.tx.QueryRow(context.Background(), `SELECT count(*) FROM exam_broadcasts WHERE session_id = $1`, e.b.ExamSessionID).Scan(&n)
	if rec.Code != http.StatusSeeOther || n != 0 {
		t.Errorf("cross-school broadcast = %d, rows %d", rec.Code, n)
	}
}

func TestControlCards(t *testing.T) {
	e := newControlEnv(t)
	ctx := context.Background()
	students := e.students(t)
	a0 := e.start(t, students[0], dev1)
	e.start(t, students[1], dev2)
	now := e.real.Add(time.Second)

	us := e.units(t, now)
	ua, ub := us[e.a.ExamRoomID], us[e.roomB]
	if ub.Color != CardRed || !strings.Contains(strings.Join(ub.Problems, ","), "Belum ada pengawas") {
		t.Errorf("room B = %s %v, want red without proctor", ub.Color, ub.Problems)
	}
	if ua.Color != CardRed || ua.ProctorOpen || ua.Present != 2 || ua.Working != 2 || ua.Total != len(students) {
		t.Errorf("room A = %+v, want red (proctor screen closed), 2 present working", ua)
	}
	// Pengawas membuka layar: hijau.
	if err := MarkProctorSeen(ctx, e.tx, e.a.ID, e.sess.ID, now); err != nil {
		t.Fatal(err)
	}
	if ua = e.units(t, now)[e.a.ExamRoomID]; ua.Color != CardGreen || !ua.ProctorOpen {
		t.Errorf("room A after proctor opened = %s %v", ua.Color, ua.Problems)
	}
	// Satu dari dua offline (> 20%): merah.
	e.tx.Exec(ctx, `UPDATE exam_attempts SET last_seen_at = $2 WHERE id = $1`, a0.ID, now.Add(-2*time.Minute))
	if ua = e.units(t, now)[e.a.ExamRoomID]; ua.Color != CardRed || ua.Offline != 1 || ua.Attention != 1 {
		t.Errorf("room A with offline = %s offline %d attention %d %v", ua.Color, ua.Offline, ua.Attention, ua.Problems)
	}
	// Kartu tampil di data langsung.
	body := e.staffDo(http.MethodGet, e.page+"/data", e.a.Admin, nil).Body.String()
	if !strings.Contains(body, "control-card is-merah") || !strings.Contains(body, "Belum ada pengawas") {
		t.Errorf("live data missing red cards")
	}
	rc, err := LoadDailyRecap(ctx, e.tx, e.a.ID, e.a.ExamEventID, mustSessions(t, e, now), now)
	if err != nil {
		t.Fatal(err)
	}
	if len(rc.Attendance) != 1 || rc.Attendance[0].Present != 2 || rc.Attendance[0].Total != len(students) {
		t.Errorf("recap attendance = %+v", rc.Attendance)
	}
}

func mustSessions(t *testing.T, e *controlEnv, now time.Time) []ControlSession {
	t.Helper()
	from, to := DayBounds(now, time.Local)
	ss, err := LoadControlSessions(context.Background(), e.tx, e.a.ID, e.a.ExamEventID, from.Add(-24*time.Hour), to.Add(24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	return ss
}

func TestControlBroadcastAndBulk(t *testing.T) {
	e := newControlEnv(t)
	ctx := context.Background()
	students := e.students(t)
	a0 := e.start(t, students[0], dev1)
	e.clock = e.real

	rec := e.staffDo(http.MethodPost, e.page+"/aksi", e.a.Admin, url.Values{"aksi": {"pesan"}, "sid": {e.sess.ID + " " + e.sessB},
		"pesan": {"Soal no. 12 opsi C dibaca \"hasil\"."}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("broadcast = %d", rec.Code)
	}
	// Peserta menerima pesan di balasan ping berikutnya.
	tok, _ := DevTokenSigner().TokenFor(a0, e.clock)
	var resp saveResponse
	rec = e.saveAs("/u/a/"+a0.ID+"/simpan", tok, `{"changes":[],"msg":0,"rtt":120}`)
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || len(resp.Msgs) != 1 || !strings.Contains(resp.Msgs[0].Body, "opsi C") {
		t.Fatalf("save response msgs = %s", rec.Body.String())
	}
	rec = e.saveAs("/u/a/"+a0.ID+"/simpan", tok, `{"changes":[],"msg":`+jsonInt(resp.Msgs[0].ID)+`}`)
	resp = saveResponse{}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Msgs) != 0 {
		t.Errorf("acknowledged message sent again: %s", rec.Body.String())
	}
	// Tercatat di berita acara kedua sesi.
	for _, sid := range []string{e.sess.ID, e.sessB} {
		m, err := LoadMinutes(ctx, e.tx, e.a.ID, sid, time.Local)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(m.Incidents, "\n"), "Pesan ke peserta: Soal no. 12") {
			t.Errorf("minutes %s missing broadcast: %v", sid, m.Incidents)
		}
	}
	if h := SaveHealth.Snapshot(e.a.ID, time.Now()); h.Saves < 2 || h.RTTCount < 1 {
		t.Errorf("health = %+v", h)
	}

	// Jeda semua sesi berlangsung, lalu lanjutkan.
	rec = e.staffDo(http.MethodPost, e.page+"/aksi", e.a.Admin, url.Values{"aksi": {"jeda"}, "target": {"semua"}, "alasan": {"listrik padam"}})
	var paused bool
	e.tx.QueryRow(ctx, `SELECT paused_at IS NOT NULL FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&paused)
	if rec.Code != http.StatusSeeOther || !paused {
		t.Fatalf("bulk pause = %d paused %v", rec.Code, paused)
	}
	e.staffDo(http.MethodPost, e.page+"/aksi", e.a.Admin, url.Values{"aksi": {"lanjut"}, "target": {"semua"}})
	e.tx.QueryRow(ctx, `SELECT paused_at IS NOT NULL FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&paused)
	if paused {
		t.Error("bulk resume left attempt paused")
	}
	m, _ := LoadMinutes(ctx, e.tx, e.a.ID, e.sessB, time.Local)
	if !strings.Contains(strings.Join(m.Incidents, "\n"), "Sesi dijeda oleh koordinator (listrik padam).") {
		t.Errorf("pause not in minutes: %v", m.Incidents)
	}
	// Tanpa sasaran: tidak ada yang berubah.
	if rec := e.staffDo(http.MethodPost, e.page+"/aksi", e.a.Admin, url.Values{"aksi": {"jeda"}}); rec.Code != http.StatusSeeOther {
		t.Errorf("no target = %d", rec.Code)
	}
}

func jsonInt(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestControlMoveParticipant(t *testing.T) {
	e := newControlEnv(t)
	ctx := context.Background()
	students := e.students(t)
	a0 := e.start(t, students[0], dev1)
	pid := e.participant(t, students[0])
	if _, err := SaveAnswers(ctx, e.tx, e.a.ID, a0.ID, dev1, SaveRequest{Pos: -1,
		Changes: []AnswerChange{{QuestionID: a0.QuestionOrder[0], Value: json.RawMessage(`"jawab"`), Seq: 1}}}, e.real); err != nil {
		t.Fatal(err)
	}
	// Sesi tujuan punya aturan waktu berbeda (+15 menit sesi).
	e.tx.Exec(ctx, `UPDATE exam_sessions SET extra_minutes = 15 WHERE id = $1`, e.sessB)
	var before time.Time
	e.tx.QueryRow(ctx, `SELECT deadline_at FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&before)

	rec := e.staffDo(http.MethodPost, e.page+"/pindah", e.a.Admin, url.Values{"peserta": {pid}, "tujuan": {e.sessB}, "alasan": {"komputer rusak"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("move = %d", rec.Code)
	}
	var pSess, aSess string
	var after time.Time
	var answers int
	var moveOK bool
	e.tx.QueryRow(ctx, `SELECT session_id FROM exam_session_participants WHERE id = $1`, pid).Scan(&pSess)
	e.tx.QueryRow(ctx, `SELECT session_id, deadline_at, device_move_allowed FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&aSess, &after, &moveOK)
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_answers WHERE attempt_id = $1 AND value <> 'null'::jsonb`, a0.ID).Scan(&answers)
	if pSess != e.sessB || aSess != e.sessB || !after.Equal(before) || answers != 1 || !moveOK {
		t.Fatalf("after move: participant %s attempt %s deadline %v→%v answers %d moveOK %v", pSess, aSess, before, after, answers, moveOK)
	}
	var creds int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM student_credentials WHERE exam_session_id = $1 AND student_id = $2 AND revoked_at IS NULL`,
		e.sessB, students[0]).Scan(&creds)
	if creds != 1 {
		t.Errorf("credential moved = %d", creds)
	}
	// Perhitungan ulang tenggat di sesi baru mempertahankan sisa waktu.
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	if err := AddSessionTime(ctx, e.tx, sa, e.sessB, 5, e.real); err != nil {
		t.Fatal(err)
	}
	e.tx.QueryRow(ctx, `SELECT deadline_at FROM exam_attempts WHERE id = $1`, a0.ID).Scan(&after)
	if !after.Equal(before.Add(5 * time.Minute)) {
		t.Errorf("deadline after add time = %v, want %v", after, before.Add(5*time.Minute))
	}
	// Perangkat baru bisa melanjutkan dengan jawaban yang sama.
	p := participantOf(e.takeEnv, students[0])
	got, err := StartAttempt(ctx, e.tx, p, StartRequest{SessionID: e.sessB, DeviceID: devProctor3, Now: e.real})
	if err != nil || got.ID != a0.ID {
		t.Fatalf("resume on new device = %v, %v", got.ID, err)
	}
	for sid, want := range map[string]string{e.sess.ID: "Dipindah ke Ruang B", e.sessB: "Pindahan dari"} {
		m, _ := LoadMinutes(ctx, e.tx, e.a.ID, sid, time.Local)
		if !strings.Contains(strings.Join(m.Incidents, "\n"), want) {
			t.Errorf("minutes %s missing %q: %v", sid, want, m.Incidents)
		}
	}
	// Pindah ke sesi yang sama atau ke sesi sekolah lain ditolak.
	if _, err := MoveParticipant(ctx, e.tx, sa, e.a.ExamEventID, pid, e.sessB, "", e.real); err != ErrMoveTarget {
		t.Errorf("move to same session = %v", err)
	}
	if _, err := MoveParticipant(ctx, e.tx, sa, e.a.ExamEventID, pid, e.b.ExamSessionID, "", e.real); err != ErrMoveTarget {
		t.Errorf("move to other school = %v", err)
	}
}

func TestControlReplaceProctor(t *testing.T) {
	e := newControlEnv(t)
	ctx := context.Background()
	other := testutil.User{ID: e.teacher2, Role: "guru"}
	if rec := e.staffDo(http.MethodGet, "/ujian/sesi/"+e.sessB+"/awasi", other, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("before replace = %d", rec.Code)
	}
	rec := e.staffDo(http.MethodPost, e.page+"/pengawas", e.a.Admin, url.Values{"sid": {e.sessB}, "user_id": {e.teacher2}, "alasan": {"pengawas sakit"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("replace = %d", rec.Code)
	}
	if rec := e.staffDo(http.MethodGet, "/ujian/sesi/"+e.sessB+"/awasi", other, nil); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Aksi sesi") {
		t.Errorf("new proctor = %d", rec.Code)
	}
	var n int
	e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_event_proctors WHERE event_id = $1 AND room_id = $2 AND user_id = $3`,
		e.a.ExamEventID, e.roomB, e.teacher2).Scan(&n)
	if n != 1 {
		t.Errorf("event proctor rows = %d", n)
	}
	// Bukan guru: ditolak.
	sa := StaffActor{Actor: Actor{UserID: e.a.Admin.ID, Roles: []string{roleAdmin}}, SchoolID: e.a.ID}
	if _, err := ReplaceProctor(ctx, e.tx, sa, e.a.ExamEventID, []string{e.sessB}, e.a.Treasurer.ID, "", e.real); err != ErrBadProctor {
		t.Errorf("non-teacher proctor = %v", err)
	}
}

func TestControlVoidQuestion(t *testing.T) {
	e := newControlEnv(t)
	ctx := context.Background()
	students := e.students(t)
	a0 := e.start(t, students[0], dev1)
	examID := e.sess.ExamID
	qid := a0.QuestionOrder[0]

	// Guru lain (bukan pembuat): tidak boleh.
	if rec := e.staffDo(http.MethodPost, "/ujian/paket/"+examID+"/soal/"+qid+"/batal", testutil.User{ID: e.teacher2, Role: "guru"},
		url.Values{"umumkan": {"1"}}); rec.Code == http.StatusSeeOther {
		t.Errorf("other teacher void = %d", rec.Code)
	}
	rec := e.staffDo(http.MethodPost, "/ujian/paket/"+examID+"/soal/"+qid+"/batal", e.a.Admin, url.Values{"alasan": {"kunci ganda"}, "umumkan": {"1"}})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("void = %d", rec.Code)
	}
	v, err := VoidedQuestions(ctx, e.tx, e.a.ID, examID)
	if err != nil || v[qid].Reason != "kunci ganda" {
		t.Fatalf("voided = %v, %v", v, err)
	}
	var body string
	e.tx.QueryRow(ctx, `SELECT body FROM exam_broadcasts WHERE session_id = $1 ORDER BY id DESC LIMIT 1`, e.sess.ID).Scan(&body)
	if !strings.Contains(body, "dibatalkan dan tidak dinilai") || !strings.Contains(body, "kunci ganda") {
		t.Errorf("void broadcast = %q", body)
	}
	page := e.staffDo(http.MethodGet, "/ujian/paket/"+examID+"?tab=soal", e.a.Admin, nil).Body.String()
	if !strings.Contains(page, "Dibatalkan") || !strings.Contains(page, "Pulihkan soal") || !strings.Contains(page, "Kirim ralat") {
		t.Errorf("soal tab missing void state or correction form")
	}
	e.staffDo(http.MethodPost, "/ujian/paket/"+examID+"/soal/"+qid+"/batal", e.a.Admin, url.Values{"aksi": {"pulihkan"}})
	if v, _ := VoidedQuestions(ctx, e.tx, e.a.ID, examID); len(v) != 0 {
		t.Errorf("restore left %v", v)
	}
	// Ralat ke sesi berlangsung.
	rec = e.staffDo(http.MethodPost, "/ujian/paket/"+examID+"/ralat", e.a.Admin, url.Values{"pesan": {"opsi C dibaca hasil"}})
	e.tx.QueryRow(ctx, `SELECT body FROM exam_broadcasts WHERE session_id = $1 ORDER BY id DESC LIMIT 1`, e.sessB).Scan(&body)
	if rec.Code != http.StatusSeeOther || body != "Ralat: opsi C dibaca hasil" {
		t.Errorf("correction = %d %q", rec.Code, body)
	}
}

func TestHealthVerdict(t *testing.T) {
	h := NewHealthRecorder()
	now := time.Now()
	if v := h.Snapshot("s", now).Verdict(); v != "tenang" {
		t.Errorf("empty = %s", v)
	}
	for i := 0; i < 100; i++ {
		h.Record("s", 40*time.Millisecond, false, 300, now)
	}
	if v := h.Snapshot("s", now).Verdict(); v != "normal" {
		t.Errorf("fast = %s", v)
	}
	for i := 0; i < 20; i++ {
		h.Record("s", 40*time.Millisecond, false, 6000, now)
	}
	if v := h.Snapshot("s", now).Verdict(); v != "jaringan" {
		t.Errorf("slow rtt = %s", v)
	}
	for i := 0; i < 5; i++ {
		h.Record("s", 40*time.Millisecond, true, 0, now)
	}
	if hs := h.Snapshot("s", now); hs.Verdict() != "server" || hs.Errors != 5 {
		t.Errorf("errors = %s %+v", hs.Verdict(), hs)
	}
	// Sampel lama tidak dihitung.
	if hs := h.Snapshot("s", now.Add(HealthWindow+time.Minute)); hs.Saves != 0 {
		t.Errorf("old samples counted: %+v", hs)
	}
}
