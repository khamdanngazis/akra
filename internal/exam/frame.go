package exam

import (
	"context"
	"fmt"
	"time"
)

// Kerangka halaman satu ujian (T160): detail, hasil, analisis, koreksi
// esai, kirim ke rapor, dan betulkan kunci memakai header, tab utama, dan
// sub-tab hasil yang sama, dengan satu tombol utama sesuai status ujian.

// frameAction adalah tombol utama header ujian.
type frameAction struct {
	Label, Href string
}

// frameState adalah keadaan ujian yang menentukan tombol utama.
type frameState struct {
	CanWrite bool
	Awaiting int  // peserta menunggu koreksi esai
	Release  bool // hasil perlu dibuka untuk siswa (belum dibuka)
	ToReport bool // ujian bisa dikirim ke rapor oleh pengguna ini
	Sent     bool // nilai pernah dikirim ke rapor
	Stale    int  // siswa yang nilainya berubah sejak dikirim
}

// PrimaryAction mengembalikan tombol utama header ujian e (satu saja),
// atau aksi kosong bila tidak ada yang perlu ditonjolkan.
func PrimaryAction(e Exam, s frameState) frameAction {
	base := examPath(e.ID)
	switch e.Status {
	case ExamDraft:
		if s.CanWrite {
			return frameAction{"Lanjutkan menyusun", examStepPath(e.ID, e.WizardStep)}
		}
	case "terjadwal":
		if s.CanWrite {
			return frameAction{"Cetak kartu", base + "?tab=dokumen"}
		}
		return frameAction{"Lihat jadwal", base + "?tab=sesi"}
	case "berlangsung":
		return frameAction{"Pantau sesi", base + "?tab=sesi"}
	case "selesai":
		switch {
		case s.CanWrite && s.Awaiting > 0:
			return frameAction{"Koreksi esai", base + "/esai"}
		case s.CanWrite && s.Release:
			return frameAction{"Buka hasil untuk siswa", base + "/hasil"}
		case s.ToReport && (!s.Sent || s.Stale > 0):
			return frameAction{"Kirim ke rapor", base + "/rapor"}
		}
		return frameAction{"Lihat hasil", base + "/hasil"}
	}
	return frameAction{}
}

// examFrame adalah isi kerangka halaman satu ujian.
type examFrame struct {
	Exam Exam
	// Tab utama: ringkasan, soal, sesi, dokumen, hasil, lanjut.
	Tab string
	// Sub: sub-tab hasil (nilai, butir, tp, esai, rapor) atau halaman di
	// bawah tab lain; SubLabel untuk breadcrumb.
	Sub, SubLabel string
	Query         string // saringan hasil (kelas, sesi, paket)
	CanWrite      bool
	ToReport      bool
	Awaiting      int
	Stale         int
	Primary       frameAction
	Next          *nextStep
}

// frame menyusun kerangka halaman ujian e untuk sa.
func (h *Handler) frame(ctx context.Context, sa StaffActor, e Exam, tab, sub, subLabel, query string) (examFrame, error) {
	now := time.Now()
	f := examFrame{Exam: e, Tab: tab, Sub: sub, SubLabel: subLabel, Query: query, CanWrite: e.Access(sa.Actor, now) >= Write}
	// Kirim ke rapor butuh modul nilai (T166).
	f.ToReport = f.CanWrite && (e.FeedsGrades() || e.FollowUp != "") && ModuleFrom(ctx).Grades
	var sent bool
	if err := h.db.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM exam_attempts t WHERE t.exam_id = $1 AND t.status = 'dikumpulkan' AND t.score_status = 'menunggu_esai'),
		EXISTS (SELECT 1 FROM exam_grade_sent g WHERE g.exam_id = $1)`, e.ID).Scan(&f.Awaiting, &sent); err != nil {
		return f, fmt.Errorf("exam: frame state: %w", err)
	}
	if f.ToReport && sent {
		var err error
		if f.Stale, err = GradesStale(ctx, h.db, sa.SchoolID, e.ID); err != nil {
			return f, err
		}
	}
	release := e.Purpose != PurposeAdmission && e.ResultsMode != ResultsNone && e.ResultsAfterAll && e.ResultsReleasedAt == nil
	f.Primary = PrimaryAction(e, frameState{CanWrite: f.CanWrite, Awaiting: f.Awaiting, Release: release, ToReport: f.ToReport, Sent: sent, Stale: f.Stale})
	return f, nil
}

// frameOrLog seperti frame, tetapi bila keadaan ujian gagal dibaca hanya
// dicatat di log dan kerangka dasar tetap dikembalikan (halaman tetap
// terbuka).
func (h *Handler) frameOrLog(ctx context.Context, sa StaffActor, e Exam, tab, sub, subLabel string) examFrame {
	f, err := h.frame(ctx, sa, e, tab, sub, subLabel, "")
	if err != nil {
		h.logger.ErrorContext(ctx, "exam frame", "exam_id", e.ID, "error", err)
	}
	return f
}
