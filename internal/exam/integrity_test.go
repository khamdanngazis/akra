package exam

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestIntegrityEvents(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	s := now.Add(time.Hour)
	ex, sess, _ := e.allKindsExam(t, s)
	p0 := participantOf(e, e.students(t)[0])
	a, err := StartAttempt(ctx, e.tx, p0, StartRequest{SessionID: sess.ID, DeviceID: dev1, Now: s})
	if err != nil {
		t.Fatal(err)
	}
	at := s.Add(5 * time.Minute)
	ms := func(d time.Duration) int64 { return at.Add(-d).UnixMilli() }
	save := func(evs ...EventInput) (SaveResult, error) {
		return SaveAnswers(ctx, e.tx, e.a.ID, a.ID, dev1, SaveRequest{Events: evs, Pos: -1}, at)
	}
	res, err := save(EventInput{Seq: 1, Kind: EventLeave, At: ms(time.Minute), Duration: 45000},
		EventInput{Seq: 2, Kind: EventPaste, At: ms(30 * time.Second)},
		EventInput{Seq: 3, Kind: EventLeave, At: ms(10 * time.Second), Duration: 5000})
	if err != nil || res.EventSeq != 3 || res.LockedAt != nil {
		t.Fatalf("save events = %+v, %v", res, err)
	}
	// Kirim ulang (balasan hilang): tidak tercatat dua kali.
	if _, err := save(EventInput{Seq: 3, Kind: EventLeave, At: ms(10 * time.Second), Duration: 5000}); err != nil {
		t.Fatal(err)
	}
	a, _ = GetAttempt(ctx, e.tx, p0, a.ID)
	if got := a.Integrity(); got.Leaves != 2 || got.Away != 50*time.Second || got.Locked {
		t.Errorf("integrity = %+v", got)
	}
	evs, err := ListAttemptEvents(ctx, e.tx, e.a.ID, a.ID, 10)
	if err != nil || len(evs) != 3 || evs[0].Kind != EventLeave || evs[0].Duration != 5*time.Second || evs[1].Label() != "Menempel teks" {
		t.Errorf("events = %+v, %v", evs, err)
	}
	// Waktu kejadian dibatasi: tidak sebelum mulai dan tidak di masa depan.
	if _, err := save(EventInput{Seq: 4, Kind: EventReload, At: 1}, EventInput{Seq: 5, Kind: EventCopy, At: at.Add(time.Hour).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	evs, _ = ListAttemptEvents(ctx, e.tx, e.a.ID, a.ID, 10)
	for _, ev := range evs {
		if ev.OccurredAt.Before(s) || ev.OccurredAt.After(at) {
			t.Errorf("event %s at %s outside attempt", ev.Kind, ev.OccurredAt)
		}
	}
	// Kejadian tidak valid ditolak; kejadian server tidak boleh dikirim
	// perangkat.
	for _, bad := range [][]EventInput{
		{{Seq: 9, Kind: "lain", At: ms(0)}},
		{{Seq: 9, Kind: EventUnlocked, At: ms(0)}},
		{{Seq: 0, Kind: EventLeave, At: ms(0)}},
		{{Seq: 9, Kind: EventLeave, At: ms(0), Duration: -1}},
		make([]EventInput, MaxEventsPerSave+1),
	} {
		if _, err := save(bad...); !errors.Is(err, ErrBadAnswer) {
			t.Errorf("bad events %v accepted: %v", bad[0], err)
		}
	}

	// Aturan kunci 2 kali (dihitung sejak sekarang: sudah 2 kali keluar,
	// kunci terjadi saat keluar berikutnya karena 3 - 0 >= 2).
	e.tx.Exec(ctx, `UPDATE exams SET integrity_mode = 'kunci', integrity_limit = 2 WHERE id = $1`, ex.ID)
	res, err = save(EventInput{Seq: 10, Kind: EventLeave, At: ms(time.Second), Duration: 2000})
	if err != nil || res.LockedAt == nil || !res.LockedAt.Equal(at) {
		t.Fatalf("lock = %+v, %v", res, err)
	}
	// Simpan tetap diterima selama dikunci; status kunci tetap terkirim.
	if res, err := save(); err != nil || res.LockedAt == nil {
		t.Errorf("save while locked = %+v, %v", res, err)
	}
	if err := UnlockAttempt(ctx, e.tx, e.owner, a.ID, at); err != nil {
		t.Fatal(err)
	}
	a, _ = GetAttempt(ctx, e.tx, p0, a.ID)
	if a.LockedAt != nil || a.LeavesBeforeLock(2) != 2 {
		t.Errorf("after unlock: locked %v, left %d", a.LockedAt, a.LeavesBeforeLock(2))
	}
	if res, _ := save(EventInput{Seq: 11, Kind: EventLeave, At: ms(0), Duration: 1000}); res.LockedAt != nil {
		t.Error("locked again after one leave")
	}
	if res, _ := save(EventInput{Seq: 12, Kind: EventLeave, At: ms(0), Duration: 1000}); res.LockedAt == nil {
		t.Error("not locked after two more leaves")
	}
	var locks, unlocks, audits int
	e.tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE kind = 'terkunci'), count(*) FILTER (WHERE kind = 'dibuka')
		FROM exam_attempt_events WHERE attempt_id = $1`, a.ID).Scan(&locks, &unlocks)
	e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = 'exam.attempt_unlock' AND entity_id = $1`, a.ID).Scan(&audits)
	if locks != 2 || unlocks != 1 || audits != 1 {
		t.Errorf("lock events %d, unlock events %d, audits %d", locks, unlocks, audits)
	}
}

func TestDeviceMove(t *testing.T) {
	e := newTakeEnv(t)
	ctx := context.Background()
	start := now.Add(time.Hour)
	_, sess, cards := e.allKindsExam(t, start)
	e.clock = start.Add(time.Minute)
	e.login(t, "hp1", cards[0])
	loc := e.form("hp1", "/u/ujian/"+sess.ID+"/mulai", url.Values{}).Header().Get("Location")
	if !strings.HasPrefix(loc, "/u/a/") {
		t.Fatalf("start → %q", loc)
	}
	id := strings.TrimPrefix(loc, "/u/a/")
	oldToken := pageState(t, e.do("hp1", http.MethodGet, loc, "", "").Body.String())["token"].(string)
	events := func(kind string) (n int) {
		e.tx.QueryRow(ctx, `SELECT count(*) FROM exam_attempt_events WHERE attempt_id = $1 AND kind = $2`, id, kind).Scan(&n)
		return
	}

	// Perangkat kedua ditolak dan dicatat (sekali per menit).
	for i := 0; i < 2; i++ {
		rec := e.form("hp2", "/u/"+e.a.Code, url.Values{"identifier": {cards[0].Identifier}, "code": {cards[0].Code}})
		if rec.Code == http.StatusSeeOther || !strings.Contains(rec.Body.String(), "perangkat lain") {
			t.Fatalf("second device login = %d", rec.Code)
		}
	}
	if n := events(EventOtherDevice); n != 1 {
		t.Errorf("other device events = %d", n)
	}

	// Pengawas mengizinkan pindah: perangkat kedua menjadi perangkat aktif.
	var cred string
	e.tx.QueryRow(ctx, `SELECT c.id FROM student_credentials c JOIN students s ON s.id = c.student_id
		WHERE s.nis = $1 AND c.exam_session_id = $2 AND c.revoked_at IS NULL`, cards[0].Identifier, sess.ID).Scan(&cred)
	if err := ResetDevice(ctx, e.tx, e.a.ID, e.owner.UserID, cred); err != nil {
		t.Fatal(err)
	}
	e.login(t, "hp2", cards[0])
	page := e.do("hp2", http.MethodGet, loc, "", "")
	if page.Code != http.StatusOK {
		t.Fatalf("new device take = %d", page.Code)
	}
	newToken := pageState(t, page.Body.String())["token"].(string)
	var moves int
	var allowed bool
	e.tx.QueryRow(ctx, `SELECT device_moves, device_move_allowed FROM exam_attempts WHERE id = $1`, id).Scan(&moves, &allowed)
	if moves != 1 || allowed || events(EventDeviceMove) != 1 {
		t.Errorf("moves %d, allowed %v, move events %d", moves, allowed, events(EventDeviceMove))
	}
	// Token perangkat lama tidak berlaku; perangkat baru menyimpan.
	if rec := e.saveAs(loc+"/simpan", oldToken, `{"changes":[]}`); rec.Code != http.StatusConflict {
		t.Errorf("old device save = %d", rec.Code)
	}
	if rec := e.saveAs(loc+"/simpan", newToken, `{"changes":[]}`); rec.Code != http.StatusOK {
		t.Errorf("new device save = %d %s", rec.Code, rec.Body.String())
	}
	// Perangkat lama membuka halaman ujian: ditolak (izin sudah terpakai).
	e.clock = e.clock.Add(2 * time.Minute)
	if rec := e.do("hp1", http.MethodGet, loc, "", ""); rec.Code != http.StatusConflict {
		t.Errorf("old device take = %d", rec.Code)
	}
	if n := events(EventOtherDevice); n != 2 {
		t.Errorf("other device events after old device opened = %d", n)
	}
}
