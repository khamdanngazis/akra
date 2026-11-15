package admission

import (
	"context"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/clock"
	"akra/internal/school"
)

// ActionPeriodCopy adalah aksi audit salin gelombang (T117).
const ActionPeriodCopy = "admission.period_copy"

// Copy membuat gelombang baru berstatus draf dari gelombang id: jenis,
// tahun ajaran, tingkat, biaya, dokumen wajib, jalur (tanpa pendaftar), dan
// tes seleksi (tanpa jadwal dan nilai). Tanggal digeser ke setelah gelombang
// asal dengan lama yang sama; nama diberi akhiran "(salinan)".
func (s *Service) Copy(ctx context.Context, actor school.Actor, id string) (Period, error) {
	var created Period
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		src, err := getPeriod(ctx, tx, actor.SchoolID, id, false)
		if err != nil {
			return err
		}
		days := int(src.ClosesOn.Sub(src.OpensOn).Hours() / 24)
		opens := src.ClosesOn.AddDate(0, 0, 1)
		if today := s.today(); opens.Before(today) {
			opens = today
		}
		closes := opens.AddDate(0, 0, days)
		var grade *int
		if src.TargetGrade > 0 {
			g := src.TargetGrade
			grade = &g
		}
		var newID string
		for n := 1; ; n++ {
			name := copyName(src.Name, n)
			err := tx.QueryRow(ctx, `
				INSERT INTO admission_periods (school_id, academic_year_id, name, kind, target_grade, opens_on, closes_on,
				       fee_amount, fee_required_before_verify, required_docs, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
				ON CONFLICT DO NOTHING RETURNING id`,
				actor.SchoolID, src.AcademicYearID, name, src.Kind, grade, opens, closes,
				src.FeeAmount, src.FeeRequiredBeforeVerify, src.RequiredDocs, actor.UserID).Scan(&newID)
			if err == nil {
				break
			}
			if !errors.Is(err, pgx.ErrNoRows) || n >= 20 {
				return mapError("copy period", err)
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO admission_tracks (school_id, period_id, name, quota, uses_test, method, score_label, score_max, sort_order)
			SELECT school_id, $3, name, quota, uses_test, method, score_label, score_max, sort_order FROM admission_tracks WHERE school_id = $1 AND period_id = $2`,
			actor.SchoolID, src.ID, newID); err != nil {
			return mapError("copy tracks", err)
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO admission_tests (school_id, period_id, name, weight, max_score, location, sort_order)
			SELECT school_id, $3, name, weight, max_score, location, sort_order FROM admission_tests WHERE school_id = $1 AND period_id = $2`,
			actor.SchoolID, src.ID, newID); err != nil {
			return mapError("copy tests", err)
		}
		if created, err = getPeriod(ctx, tx, actor.SchoolID, newID, false); err != nil {
			return err
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionPeriodCopy, newID, map[string]any{"source_id": src.ID}, periodSnapshot(created)))
	})
	if err != nil {
		return Period{}, err
	}
	return created, nil
}

// copyName membuat nama salinan yang muat 80 karakter.
func copyName(name string, n int) string {
	suffix := " (salinan)"
	if n > 1 {
		suffix = fmt.Sprintf(" (salinan %d)", n)
	}
	for utf8.RuneCountInString(name)+utf8.RuneCountInString(suffix) > 80 {
		r := []rune(name)
		name = string(r[:len(r)-1])
	}
	return name + suffix
}

// checkItem adalah satu syarat sebelum gelombang dibuka.
type checkItem struct {
	Label, Hint, FixURL, FixLabel string
	OK                            bool
}

// periodChecklist menyusun daftar periksa kesiapan membuka gelombang
// (T117). Semua butir harus OK sebelum gelombang bisa dibuka.
func periodChecklist(p Period, tests []SelectionTest, today time.Time) (items []checkItem, ready bool) {
	base := periodsPath + "/" + p.ID
	items = append(items, checkItem{Label: "Minimal satu jalur pendaftaran", OK: len(p.Tracks) > 0,
		Hint: "Calon siswa memilih jalur saat mendaftar.", FixURL: base + "/atur/jalur", FixLabel: "Tambah jalur"})
	usesTest := false
	for _, t := range p.Tracks {
		usesTest = usesTest || t.UsesTest
	}
	if usesTest {
		total := WeightsTotal(tests)
		items = append(items, checkItem{Label: "Tes seleksi dengan bobot total 100%", OK: len(tests) > 0 && total == 100,
			Hint: fmt.Sprintf("Ada jalur yang memakai tes. Bobot sekarang %d%%.", total), FixURL: base + "/tes", FixLabel: "Atur tes"})
	}
	items = append(items, checkItem{Label: "Tanggal tutup belum lewat", OK: !p.ClosesOn.Before(today),
		Hint: "Tutup " + clock.FormatDateID(p.ClosesOn) + ".", FixURL: base + "/atur/jadwal", FixLabel: "Ubah jadwal"})
	ready = true
	for _, it := range items {
		ready = ready && it.OK
	}
	return items, ready
}
