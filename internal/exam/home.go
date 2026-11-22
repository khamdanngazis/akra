package exam

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// Beranda modul ujian (T158): apa yang terjadi hari ini dan apa yang perlu
// ditindaklanjuti pengguna, sesuai hak aksesnya.

// MaxToDo adalah jumlah baris "Perlu tindakan" maksimal.
const MaxToDo = 8

// todoWindow: ujian yang sesi terakhirnya berakhir lebih lama dari ini
// tidak lagi diingatkan (hasil belum dibuka, nilai belum dikirim).
const todoWindow = 60 * 24 * time.Hour

// ToDo adalah satu baris "Perlu tindakan".
type ToDo struct {
	Text   string // mis. "Koreksi 12 jawaban esai"
	Detail string // mis. nama ujian
	Action string // label tombol
	Href   string
	rank   int // makin kecil makin mendesak
}

// TodayEvent adalah agenda PTS/PAS yang berlangsung hari ini.
type TodayEvent struct {
	ID, Name string
}

// MyToDo menyusun "Perlu tindakan" untuk sa: ujian miliknya (draf, hasil
// belum dibuka, nilai belum/perlu dikirim ulang ke rapor), esai yang
// menunggu koreksinya, soal menunggu telaah (review: telaah aktif), dan
// untuk pengelola agenda: agenda dekat yang belum diumumkan atau sesinya
// belum berpengawas.
func MyToDo(ctx context.Context, q database.DBTX, sa StaffActor, review bool, now time.Time) ([]ToDo, error) {
	var out []ToDo
	rows, err := q.Query(ctx, `
		WITH mine AS (
			SELECT e.id, e.name, e.status, e.purpose, e.subject_id, e.results_mode, e.results_after_all, e.results_released_at
			FROM exams e WHERE e.school_id = $1 AND e.owner_id = $2 AND e.archived_at IS NULL AND NOT e.builtin),
		ended AS (
			SELECT m.id FROM mine m
			WHERE EXISTS (SELECT 1 FROM exam_sessions x WHERE x.exam_id = m.id AND x.cancelled_at IS NULL)
			  AND NOT EXISTS (SELECT 1 FROM exam_sessions x WHERE x.exam_id = m.id AND x.cancelled_at IS NULL AND x.closed_at IS NULL
			                  AND x.ends_at + make_interval(mins => x.extra_minutes) > $3)
			  AND (SELECT max(x.ends_at) FROM exam_sessions x WHERE x.exam_id = m.id AND x.cancelled_at IS NULL) > $4)
		SELECT 'draf', m.id::text, m.name FROM mine m WHERE m.status = 'draf'
		UNION ALL
		SELECT 'hasil', m.id::text, m.name FROM mine m JOIN ended USING (id)
		WHERE m.purpose <> 'seleksi' AND m.results_mode <> 'tidak' AND m.results_after_all AND m.results_released_at IS NULL
		UNION ALL
		SELECT 'rapor', m.id::text, m.name FROM mine m JOIN ended USING (id)
		WHERE m.purpose IN ('ulangan', 'pts', 'pas') AND m.subject_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM exam_grade_sent g WHERE g.exam_id = m.id)
		  AND EXISTS (SELECT 1 FROM exam_attempts t WHERE t.exam_id = m.id AND t.score_status = 'final')
		UNION ALL
		SELECT 'kirim_ulang', m.id::text, m.name FROM mine m
		WHERE EXISTS (SELECT 1 FROM exam_grade_syncs s WHERE s.exam_id = m.id AND s.sent_at > $4)`,
		sa.SchoolID, sa.UserID, now, now.Add(-todoWindow))
	if err != nil {
		return nil, fmt.Errorf("exam: my to-do: %w", err)
	}
	type row struct{ kind, id, name string }
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.kind, &x.id, &x.name)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: my to-do: %w", err)
	}
	for _, x := range list {
		base := examPath(x.id)
		switch x.kind {
		case "draf":
			out = append(out, ToDo{Text: "Ujian masih draf", Detail: x.name, Action: "Lanjutkan", Href: base + "/atur/", rank: 6})
		case "hasil":
			out = append(out, ToDo{Text: "Hasil belum dibuka untuk siswa", Detail: x.name, Action: "Buka hasil", Href: base + "/hasil", rank: 3})
		case "rapor":
			out = append(out, ToDo{Text: "Nilai belum dikirim ke rapor", Detail: x.name, Action: "Kirim ke rapor", Href: base + "/rapor", rank: 4})
		case "kirim_ulang":
			// Nilai berubah setelah dikirim (nilai ulang, koreksi esai).
			n, err := GradesStale(ctx, q, sa.SchoolID, x.id)
			if err != nil {
				return nil, err
			}
			if n > 0 {
				out = append(out, ToDo{Text: "Nilai " + strconv.Itoa(n) + " siswa berubah setelah dikirim ke rapor", Detail: x.name, Action: "Kirim ulang", Href: base + "/rapor", rank: 4})
			}
		}
	}

	essays, err := MyEssayWork(ctx, q, sa.SchoolID, sa.UserID, now)
	if err != nil {
		return nil, err
	}
	for _, w := range essays {
		if w.Pending > 0 {
			out = append(out, ToDo{Text: "Koreksi " + strconv.Itoa(w.Pending) + " jawaban esai", Detail: w.ExamName, Action: "Koreksi", Href: examPath(w.ExamID) + "/esai", rank: 2})
		}
	}

	if review && sa.CanAny(ActBank, Read) {
		qs, err := ReviewQueue(ctx, q, sa, now)
		if err != nil {
			return nil, err
		}
		if len(qs) > 0 {
			out = append(out, ToDo{Text: strconv.Itoa(len(qs)) + " soal menunggu telaah", Action: "Telaah", Href: "/ujian/telaah", rank: 5})
		}
	}

	if sa.CanAny(ActAgenda, Write) {
		ev, err := q.Query(ctx, `
			SELECT v.id::text, v.name, v.announced_at IS NULL,
				(SELECT count(*) FROM exam_sessions x
				 WHERE x.event_id = v.id AND x.cancelled_at IS NULL AND x.ends_at > $2
				   AND NOT EXISTS (SELECT 1 FROM exam_session_proctors p WHERE p.session_id = x.id))
			FROM exam_events v
			WHERE v.school_id = $1 AND v.archived_at IS NULL AND v.ends_on >= $3::date AND v.starts_on <= $3::date + 14
			ORDER BY v.starts_on`, sa.SchoolID, now, now)
		if err != nil {
			return nil, fmt.Errorf("exam: agenda to-do: %w", err)
		}
		type evRow struct {
			id, name   string
			unannounce bool
			noProctor  int64
		}
		evs, err := pgx.CollectRows(ev, func(r pgx.CollectableRow) (evRow, error) {
			var x evRow
			return x, r.Scan(&x.id, &x.name, &x.unannounce, &x.noProctor)
		})
		if err != nil {
			return nil, fmt.Errorf("exam: agenda to-do: %w", err)
		}
		for _, x := range evs {
			if x.unannounce {
				out = append(out, ToDo{Text: "Jadwal belum diumumkan ke siswa", Detail: x.name, Action: "Buka agenda", Href: eventPath(x.id), rank: 1})
			}
			if x.noProctor > 0 {
				out = append(out, ToDo{Text: strconv.FormatInt(x.noProctor, 10) + " sesi belum punya pengawas", Detail: x.name, Action: "Atur pengawas", Href: eventPath(x.id) + "/pengawas", rank: 1})
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].rank < out[j].rank })
	if len(out) > MaxToDo {
		out = out[:MaxToDo]
	}
	return out, nil
}

// TodayEvents mengembalikan agenda PTS/PAS sekolah yang berlangsung hari ini.
func TodayEvents(ctx context.Context, q database.DBTX, schoolID string, today time.Time) ([]TodayEvent, error) {
	rows, err := q.Query(ctx, `SELECT id::text, name FROM exam_events WHERE school_id = $1 AND archived_at IS NULL
		AND starts_on <= $2::date AND ends_on >= $2::date ORDER BY starts_on, name`, schoolID, today)
	if err != nil {
		return nil, fmt.Errorf("exam: today events: %w", err)
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (TodayEvent, error) {
		var e TodayEvent
		return e, r.Scan(&e.ID, &e.Name)
	})
	if err != nil {
		return nil, fmt.Errorf("exam: today events: %w", err)
	}
	return out, nil
}
