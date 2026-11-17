package exam

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// Tingkat butir cek kesiapan (T133/T136): merah memblokir Jadwalkan,
// kuning hanya peringatan.
const (
	ReadyOK    = "ok"
	ReadyWarn  = "warn"
	ReadyBlock = "block"
)

// ReadyItem adalah satu butir cek kesiapan.
type ReadyItem struct {
	Key      string
	Label    string
	Hint     string
	Level    string
	FixURL   string
	FixLabel string
}

// Readiness adalah hasil cek kesiapan ujian.
type Readiness struct {
	Items   []ReadyItem
	Blocked bool
	Warns   int
}

func (r *Readiness) add(it ReadyItem) {
	r.Items = append(r.Items, it)
	switch it.Level {
	case ReadyBlock:
		r.Blocked = true
	case ReadyWarn:
		r.Warns++
	}
}

// Item mengembalikan butir dengan kunci key (untuk uji dan tampilan).
func (r Readiness) Item(key string) (ReadyItem, bool) {
	for _, it := range r.Items {
		if it.Key == key {
			return it, true
		}
	}
	return ReadyItem{}, false
}

// readyFacts adalah data yang dibaca dari database untuk cek kesiapan.
type readyFacts struct {
	// Per soal yang dipakai varian mana pun.
	mathErrors    int // soal/stimulus dengan rumus tidak valid
	missingImages int // gambar yang dirujuk tetapi tidak ada
	noKey         int // soal non-esai tanpa kunci
	archived      int // soal sudah diarsipkan di bank
	notReady      int // soal berstatus draf/ditelaah
	inReview      int
	questions     int
}

func loadReadyFacts(ctx context.Context, q database.DBTX, e Exam, plan Plan) (readyFacts, error) {
	var f readyFacts
	ids := map[string]bool{}
	for _, a := range plan.Assembled {
		for _, it := range a.Items {
			ids[it.QuestionID] = true
		}
	}
	// Soal tetap yang diarsipkan tidak ikut dirakit; hitung dari isi.
	for _, s := range plan.Sections {
		for _, it := range s.Items {
			if it.Archived {
				f.archived++
			}
		}
	}
	if len(ids) == 0 {
		return f, nil
	}
	list := make([]string, 0, len(ids))
	for id := range ids {
		list = append(list, id)
	}
	rows, err := q.Query(ctx, `SELECT q.kind, q.answer_key, q.status,
		       q.body_html || q.options::text || q.explanation_html || coalesce(st.body_html, ''),
		       q.body_src || q.options::text || q.explanation_src || coalesce(st.body_src, '')
		FROM exam_questions q LEFT JOIN exam_stimuli st ON st.id = q.stimulus_id
		WHERE q.school_id = $1 AND q.id = ANY($2)`, e.SchoolID, list)
	if err != nil {
		return f, fmt.Errorf("exam: readiness questions: %w", err)
	}
	defer rows.Close()
	assets := map[string]bool{}
	for rows.Next() {
		var kind Kind
		var raw []byte
		var status, html, src string
		if err := rows.Scan(&kind, &raw, &status, &html, &src); err != nil {
			return f, fmt.Errorf("exam: readiness questions: %w", err)
		}
		if strings.Contains(html, "math-error") {
			f.mathErrors++
		}
		for _, m := range assetRefRe.FindAllStringSubmatch(src, -1) {
			assets[m[1]] = true
		}
		f.questions++
		var key AnswerKey
		if err := json.Unmarshal(raw, &key); err != nil {
			return f, fmt.Errorf("exam: decode answer key: %w", err)
		}
		if kind != KindEssay && !key.present(kind) {
			f.noKey++
		}
		switch status {
		case StatusDraft:
			f.notReady++
		case StatusInReview:
			f.notReady++
			f.inReview++
		}
	}
	if err := rows.Err(); err != nil {
		return f, err
	}
	rows.Close()
	if len(assets) > 0 {
		ids := make([]string, 0, len(assets))
		for id := range assets {
			ids = append(ids, id)
		}
		var found int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM exam_assets WHERE school_id = $1 AND id = ANY($2)`, e.SchoolID, ids).Scan(&found); err != nil {
			return f, fmt.Errorf("exam: readiness assets: %w", err)
		}
		f.missingImages = len(ids) - found
	}
	return f, nil
}

// assetRefRe menemukan gambar yang dirujuk di sumber teks kaya (termasuk
// di dalam JSON opsi, dengan kutip ter-escape).
var assetRefRe = regexp.MustCompile(`data-asset=\\?"([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})`)

// present melaporkan apakah kunci jenis kind terisi.
func (k AnswerKey) present(kind Kind) bool {
	switch kind {
	case KindMC, KindMulti:
		return len(k.Correct) > 0
	case KindTF:
		return len(k.Truth) > 0
	case KindShort:
		return len(k.Accepted) > 0
	case KindMatch:
		return len(k.Pairs) > 0
	}
	return true
}

// CheckReadiness menyusun cek kesiapan ujian e dari rencana rakit dan sesi.
func CheckReadiness(ctx context.Context, q database.DBTX, e Exam, plan Plan, sessions []Session, settings ExamSettings, now time.Time) (Readiness, error) {
	var r Readiness
	base := "/ujian/paket/" + e.ID
	f, err := loadReadyFacts(ctx, q, e, plan)
	if err != nil {
		return r, err
	}
	comp := plan.Composition()
	soal := base + "/atur/" + stepQuestions

	it := ReadyItem{Key: "questions", Label: "Ujian berisi soal", FixURL: soal, FixLabel: "Tambah soal"}
	if comp.Count == 0 {
		it.Level, it.Hint = ReadyBlock, "Belum ada soal. Pilih dari bank atau tambahkan aturan ambil acak."
	} else {
		it.Level, it.Hint = ReadyOK, fmt.Sprintf("%d soal, total %d poin.", comp.Count, comp.Points)
	}
	r.add(it)

	it = ReadyItem{Key: "rules", Label: "Aturan acak terpenuhi untuk setiap paket", FixURL: soal, FixLabel: "Ubah aturan", Level: ReadyOK}
	if len(plan.Assembled) > 0 && len(plan.Assembled[0].Short) > 0 {
		it.Level, it.Hint = ReadyBlock, "Ada aturan yang meminta lebih banyak soal dari isi bank."
	} else {
		shared := 0
		for _, a := range plan.Assembled[min(1, len(plan.Assembled)):] {
			shared += a.Shared
		}
		if shared > 0 {
			it.Level, it.Hint = ReadyWarn, fmt.Sprintf("%d soal sama di beberapa paket karena bank tidak punya padanan dengan komposisi sama.", shared)
		}
	}
	r.add(it)

	it = ReadyItem{Key: "keys", Label: "Semua soal punya kunci jawaban", FixURL: soal, FixLabel: "Periksa soal", Level: ReadyOK}
	if f.noKey > 0 {
		it.Level, it.Hint = ReadyBlock, fmt.Sprintf("%d soal belum punya kunci.", f.noKey)
	}
	r.add(it)

	it = ReadyItem{Key: "math", Label: "Semua rumus valid", FixURL: soal, FixLabel: "Periksa soal", Level: ReadyOK}
	if f.mathErrors > 0 {
		it.Level, it.Hint = ReadyBlock, fmt.Sprintf("%d soal memuat rumus yang tidak bisa ditampilkan.", f.mathErrors)
	}
	r.add(it)
	it = ReadyItem{Key: "images", Label: "Semua gambar terunggah", FixURL: soal, FixLabel: "Periksa soal", Level: ReadyOK}
	if f.missingImages > 0 {
		it.Level, it.Hint = ReadyBlock, fmt.Sprintf("%d gambar soal tidak ditemukan.", f.missingImages)
	}
	r.add(it)

	if f.archived > 0 {
		r.add(ReadyItem{Key: "archived", Label: "Tidak ada soal yang sudah diarsipkan", Level: ReadyBlock, FixURL: soal, FixLabel: "Lepas soal",
			Hint: fmt.Sprintf("%d soal di ujian ini sudah diarsipkan di bank.", f.archived)})
	}

	it = ReadyItem{Key: "status", Label: "Soal berstatus siap", FixURL: soal, FixLabel: "Periksa soal", Level: ReadyOK}
	if f.notReady > 0 {
		it.Level, it.Hint = ReadyWarn, fmt.Sprintf("%d soal masih draf atau ditelaah.", f.notReady)
		// PTS/PAS dengan telaah aktif: soal wajib sudah disetujui.
		if e.Purpose.SchoolWide() && settings.Review {
			it.Level, it.Hint = ReadyBlock, fmt.Sprintf("%d soal belum disetujui penelaah. PTS/PAS hanya memakai soal yang sudah ditelaah.", f.notReady)
		}
	}
	r.add(it)

	if e.DurationMinutes > 0 && comp.Minutes > e.DurationMinutes {
		r.add(ReadyItem{Key: "time", Label: "Durasi cukup untuk jumlah soal", Level: ReadyWarn, FixURL: base + "/atur/" + stepBasics, FixLabel: "Ubah durasi",
			Hint: fmt.Sprintf("Perkiraan waktu mengerjakan %d menit, durasi ujian %d menit.", comp.Minutes, e.DurationMinutes)})
	}

	jadwal := base + "/atur/" + stepSchedule
	if e.Purpose.SchoolWide() {
		lv, hint := ReadyOK, fmt.Sprintf("%d sesi dari agenda.", len(sessions))
		if len(sessions) == 0 {
			lv, hint = ReadyWarn, "PTS/PAS dijadwalkan koordinator lewat agenda ujian."
		}
		r.add(ReadyItem{Key: "sessions", Label: "Sesi dan peserta", Level: lv, Hint: hint})
	} else {
		it = ReadyItem{Key: "sessions", Label: "Ada sesi dengan peserta", FixURL: jadwal, FixLabel: "Atur jadwal", Level: ReadyOK}
		total := 0
		empty := 0
		for _, s := range sessions {
			total += s.Participants
			if s.Participants == 0 {
				empty++
			}
		}
		switch {
		case len(sessions) == 0:
			it.Level, it.Hint = ReadyBlock, "Belum ada sesi."
		case total == 0:
			it.Level, it.Hint = ReadyBlock, "Sesi belum punya peserta."
		case empty > 0:
			it.Level, it.Hint = ReadyWarn, fmt.Sprintf("%d sesi tanpa peserta.", empty)
		default:
			it.Hint = fmt.Sprintf("%d sesi, %d peserta.", len(sessions), total)
		}
		r.add(it)
	}

	it = ReadyItem{Key: "window", Label: "Durasi muat di jendela setiap sesi", FixURL: jadwal, FixLabel: "Atur jadwal", Level: ReadyOK}
	short := 0
	for _, s := range sessions {
		if s.EndsAt.Sub(s.StartsAt) < time.Duration(e.DurationMinutes)*time.Minute {
			short++
		}
	}
	if short > 0 {
		it.Level, it.Hint = ReadyBlock, fmt.Sprintf("%d sesi lebih pendek dari durasi ujian (%d menit).", short, e.DurationMinutes)
	}
	r.add(it)
	if len(sessions) == 0 {
		return r, nil
	}
	if err := sessionReadiness(ctx, q, e, sessions, &r, jadwal, now); err != nil {
		return r, err
	}
	return r, nil
}

// sessionReadiness menambahkan butir sesi (T136): pengawas, kartu, zona
// waktu, dan ujian latihan.
func sessionReadiness(ctx context.Context, q database.DBTX, e Exam, sessions []Session, r *Readiness, jadwal string, now time.Time) error {
	noProctor := 0
	ids := make([]string, 0, len(sessions))
	cardsDue := false
	var opens time.Time
	for _, s := range sessions {
		ids = append(ids, s.ID)
		if s.Location == LocationRoom && len(s.ProctorIDs) == 0 {
			noProctor++
		}
		if s.StartsAt.Sub(now) <= CardLead {
			cardsDue = true
		}
		if o := s.StartsAt.Add(-CardLead); opens.IsZero() || o.Before(opens) {
			opens = o
		}
	}
	it := ReadyItem{Key: "proctors", Label: "Pengawas di setiap sesi di ruang", FixURL: jadwal, FixLabel: "Atur jadwal", Level: ReadyOK}
	if e.Purpose.SchoolWide() {
		it.FixURL, it.FixLabel = "/ujian/agenda", "Agenda"
	}
	if noProctor > 0 {
		it.Level, it.Hint = ReadyBlock, fmt.Sprintf("%d sesi di ruang belum punya pengawas.", noProctor)
	}
	r.add(it)

	loc, err := clock.ForSchool(ctx, q, e.SchoolID)
	if err != nil {
		return err
	}
	var withoutCard int
	if err := q.QueryRow(ctx, `SELECT count(*) FROM exam_session_participants p WHERE p.session_id = ANY($1) AND NOT EXISTS (
		SELECT 1 FROM student_credentials c WHERE c.exam_session_id = p.session_id AND c.revoked_at IS NULL
		  AND c.student_id IS NOT DISTINCT FROM p.student_id AND c.applicant_id IS NOT DISTINCT FROM p.applicant_id)`, ids).Scan(&withoutCard); err != nil {
		return fmt.Errorf("exam: readiness cards: %w", err)
	}
	it = ReadyItem{Key: "cards", Label: "Setiap peserta punya kartu dan kode", FixURL: examPath(e.ID) + "?tab=dokumen", FixLabel: "Cetak kartu", Level: ReadyOK}
	switch {
	case withoutCard == 0:
	case cardsDue:
		it.Level, it.Hint = ReadyWarn, fmt.Sprintf("%d peserta belum punya kartu.", withoutCard)
	default:
		it.Hint = "Kartu bisa dicetak mulai " + clock.FormatDateTimeID(opens, loc) + " (H-3)."
	}
	r.add(it)
	r.add(ReadyItem{Key: "zone", Label: "Jadwal memakai zona waktu sekolah", Level: ReadyOK,
		Hint: "Semua jam ditampilkan dalam " + clock.ZoneLabel(loc) + ". Ubah zona di profil sekolah bila salah.", FixURL: "/pengaturan/sekolah/"})

	if e.Purpose == PurposePractice || e.Purpose == PurposeAdmission {
		return nil
	}
	var notPracticed int
	if err := q.QueryRow(ctx, `
		SELECT count(DISTINCT p.student_id) FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id
		WHERE p.session_id = ANY($1) AND p.student_id IS NOT NULL AND NOT EXISTS (
			SELECT 1 FROM exam_session_participants pp
			JOIN exam_sessions xx ON xx.id = pp.session_id AND xx.cancelled_at IS NULL AND xx.starts_at < x.starts_at
			JOIN exams ee ON ee.id = xx.exam_id AND ee.purpose = 'latihan'
			WHERE pp.student_id = p.student_id)`, ids).Scan(&notPracticed); err != nil {
		return fmt.Errorf("exam: readiness practice: %w", err)
	}
	it = ReadyItem{Key: "practice", Label: "Siswa sudah mencoba ujian latihan", FixURL: "/ujian/latihan", FixLabel: "Buka latihan", Level: ReadyOK}
	if notPracticed > 0 {
		it.Level, it.Hint = ReadyWarn, fmt.Sprintf("%d siswa belum pernah ujian latihan sebelum ujian ini. Disarankan sekali sebelum ujian online pertama.", notPracticed)
	}
	r.add(it)
	return nil
}

// ErrNotReady: cek kesiapan masih punya butir merah.
var ErrNotReady = errors.New("exam: exam not ready")

// ScheduleExam menjalankan cek kesiapan lalu menjadwalkan ujian: varian
// dirakit ulang, varian dibagikan ke peserta, dan status menjadi
// terjadwal. Butir merah memblokir.
func ScheduleExam(ctx context.Context, db database.DB, sa StaffActor, id string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		e, err := loadForWrite(ctx, tx, sa, id, now, true)
		if err != nil {
			return err
		}
		plan, err := Reassemble(ctx, tx, e)
		if err != nil {
			return err
		}
		sessions, err := ListSessions(ctx, tx, sa.SchoolID, e.ID)
		if err != nil {
			return err
		}
		st, err := LoadExamSettings(ctx, tx, sa.SchoolID)
		if err != nil {
			return err
		}
		rd, err := CheckReadiness(ctx, tx, e, plan, sessions, st, now)
		if err != nil {
			return err
		}
		if rd.Blocked {
			return ErrNotReady
		}
		if err := assignParticipantVariants(ctx, tx, e.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exams SET status = 'terjadwal', scheduled_at = coalesce(scheduled_at, now()), updated_at = now()
			WHERE id = $1`, e.ID); err != nil {
			return fmt.Errorf("exam: schedule: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.schedule", "exam", e.ID, nil, nil)
	})
}
