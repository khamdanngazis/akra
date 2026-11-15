package admission

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/school"
)

// ActionApplicantStatus dicatat setiap staf mengubah status pendaftar.
const ActionApplicantStatus = "admission.applicant_status"

// StatusLabels adalah nama status pendaftar untuk ditampilkan.
var StatusLabels = map[string]string{
	ApplicantDraft:     "Draf",
	ApplicantSubmitted: "Dikirim",
	ApplicantFix:       "Perlu perbaikan",
	ApplicantVerified:  "Diverifikasi",
	ApplicantAccepted:  "Diterima",
	ApplicantReserve:   "Cadangan",
	ApplicantRejected:  "Ditolak",
	ApplicantWithdrawn: "Mundur",
}

// ApplicantStatuses adalah status yang tampil di filter daftar, urut alur.
var ApplicantStatuses = []string{ApplicantSubmitted, ApplicantFix, ApplicantVerified, ApplicantAccepted, ApplicantReserve, ApplicantRejected, ApplicantWithdrawn, ApplicantDraft}

// staffTransitions adalah perpindahan status yang boleh dilakukan staf.
// Kirim dan kirim ulang dilakukan pendaftar lewat Submit.
var staffTransitions = map[string][]string{
	ApplicantSubmitted: {ApplicantFix, ApplicantVerified, ApplicantWithdrawn},
	ApplicantFix:       {ApplicantWithdrawn},
	ApplicantVerified:  {ApplicantAccepted, ApplicantReserve, ApplicantRejected, ApplicantWithdrawn},
	ApplicantReserve:   {ApplicantAccepted, ApplicantRejected, ApplicantWithdrawn},
	ApplicantAccepted:  {ApplicantWithdrawn},
	ApplicantDraft:     {ApplicantWithdrawn},
}

// CanTransition melaporkan apakah staf boleh memindahkan status from ke to.
// Pendaftar yang sudah menjadi siswa (T96) tidak bisa diubah lagi; itu
// dicek di Transition karena butuh data.
func CanTransition(from, to string) bool {
	for _, t := range staffTransitions[from] {
		if t == to {
			return true
		}
	}
	return false
}

// NextStatuses mengembalikan status tujuan yang boleh dipilih staf.
func NextStatuses(from string) []string { return staffTransitions[from] }

var (
	// ErrBadTransition dikembalikan untuk perpindahan status yang tidak
	// diizinkan.
	ErrBadTransition = errors.New("admission: status transition not allowed")
	// ErrFeeUnpaid dikembalikan jika periode mewajibkan biaya formulir lunas
	// sebelum verifikasi (T94).
	ErrFeeUnpaid = errors.New("admission: registration fee not paid")
)

// TransitionResult adalah hasil perubahan status.
type TransitionResult struct {
	Applicant Applicant
	// ResumeCode diisi saat status menjadi perlu_perbaikan: kode lanjut baru
	// untuk diberikan ke orang tua (ditampilkan sekali).
	ResumeCode string
	// OverQuota bernilai true jika jumlah diterima di jalur ini melebihi
	// kuota setelah perubahan (peringatan, tidak diblok).
	OverQuota bool
}

// Event adalah satu baris riwayat status.
type Event struct {
	From, To  string
	Note      string
	ActorName string // kosong = pendaftar
	At        time.Time
}

// Transition mengubah status pendaftar oleh staf. Status perlu_perbaikan
// wajib dengan catatan.
func (s *ApplicantService) Transition(ctx context.Context, actor school.Actor, id, to, note string) (TransitionResult, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > 1000 {
		return TransitionResult{}, fieldErr("note", "Catatan maksimal 1.000 karakter.")
	}
	if to == ApplicantFix && note == "" {
		return TransitionResult{}, fieldErr("note", "Tulis bagian yang perlu diperbaiki agar orang tua tahu.")
	}
	var res TransitionResult
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		a, err := s.load(ctx, tx, actor.SchoolID, id, true)
		if err != nil {
			return err
		}
		if a.AnonymizedAt != nil || !CanTransition(a.Status, to) {
			return ErrBadTransition // data pribadi sudah dihapus (T113)
		}
		var studentID *string
		if err := tx.QueryRow(ctx, `SELECT student_id::text FROM applicants WHERE id = $1`, id).Scan(&studentID); err != nil {
			return err
		}
		if studentID != nil {
			return ErrBadTransition // sudah menjadi siswa
		}
		if to == ApplicantVerified {
			if err := s.checkFee(ctx, tx, a); err != nil {
				return err
			}
		}
		set := `status = $2, updated_at = now()`
		args := []any{id, to}
		if to == ApplicantFix {
			code, err := newResumeCode()
			if err != nil {
				return err
			}
			res.ResumeCode = code
			set += `, resume_code_hash = $3, resume_failures = 0, resume_locked_until = NULL`
			args = append(args, hashCode(id, code))
		}
		if _, err := tx.Exec(ctx, `UPDATE applicants SET `+set+` WHERE id = $1`, args...); err != nil {
			return err
		}
		if err := recordEvent(ctx, tx, actor.SchoolID, id, a.Status, to, note, actor.UserID); err != nil {
			return err
		}
		if err := audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionApplicantStatus, EntityType: "applicant", EntityID: id,
			Before: map[string]any{"status": a.Status}, After: map[string]any{"status": to, "has_note": note != ""},
		}); err != nil {
			return err
		}
		if to == ApplicantAccepted {
			var quota, accepted int
			err := tx.QueryRow(ctx, `SELECT coalesce(t.quota, 0),
				(SELECT count(*) FROM applicants x WHERE x.track_id = t.id AND x.status = 'diterima')
				FROM admission_tracks t WHERE t.id = $1`, a.TrackID).Scan(&quota, &accepted)
			if err != nil {
				return err
			}
			res.OverQuota = quota > 0 && accepted > quota
		}
		res.Applicant, err = s.load(ctx, tx, actor.SchoolID, id, false)
		return err
	})
	if err != nil {
		return TransitionResult{}, mapApplicantError(err)
	}
	return res, nil
}

// checkFee menolak verifikasi jika periode mewajibkan biaya formulir lunas
// dan biaya pendaftar belum lunas (T94).
func (s *ApplicantService) checkFee(ctx context.Context, tx pgx.Tx, a Applicant) error {
	if s.feePaid == nil {
		return nil
	}
	var required bool
	var fee int64
	if err := tx.QueryRow(ctx, `SELECT fee_required_before_verify, fee_amount FROM admission_periods WHERE id = $1`, a.PeriodID).Scan(&required, &fee); err != nil {
		return err
	}
	if !required || fee == 0 {
		return nil
	}
	paid, err := s.feePaid(ctx, tx, a)
	if err != nil {
		return err
	}
	if !paid {
		return ErrFeeUnpaid
	}
	return nil
}

func recordEvent(ctx context.Context, q database.DBTX, schoolID, applicantID, from, to, note, actorID string) error {
	_, err := q.Exec(ctx, `INSERT INTO applicant_events (school_id, applicant_id, from_status, to_status, note, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6)`, schoolID, applicantID, from, to, nullIfEmpty(note), nullIfEmpty(actorID))
	if err != nil {
		return fmt.Errorf("admission: record event: %w", err)
	}
	return nil
}

// Events mengembalikan riwayat status pendaftar, terlama dulu.
func (s *ApplicantService) Events(ctx context.Context, schoolID, id string) ([]Event, error) {
	rows, err := s.db.Query(ctx, `SELECT e.from_status, e.to_status, coalesce(e.note, ''), coalesce(u.full_name, ''), e.created_at
		FROM applicant_events e LEFT JOIN users u ON u.id = e.actor_id AND u.school_id = e.school_id
		WHERE e.school_id = $1 AND e.applicant_id = $2 ORDER BY e.created_at, e.id`, schoolID, id)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) {
		var e Event
		return e, r.Scan(&e.From, &e.To, &e.Note, &e.ActorName, &e.At)
	})
	if err != nil {
		return nil, mapApplicantError(err)
	}
	return list, nil
}

// LastNote mengembalikan catatan perbaikan terakhir (untuk halaman status
// pendaftar, T95).
func (s *ApplicantService) LastNote(ctx context.Context, schoolID, id string) (string, error) {
	var note string
	err := s.db.QueryRow(ctx, `SELECT coalesce(note, '') FROM applicant_events WHERE school_id = $1 AND applicant_id = $2 AND to_status = 'perlu_perbaikan'
		ORDER BY created_at DESC, id DESC LIMIT 1`, schoolID, id).Scan(&note)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return note, err
}

// ListFilter adalah filter daftar pendaftar.
type ListFilter struct {
	PeriodID string
	TrackID  string
	Status   string
	Kind     string // KindNew/KindTransfer; kosong = semua (T109)
	Query    string // nama mengandung, atau nomor pendaftaran diawali
}

// ListRow adalah satu baris daftar pendaftar.
type ListRow struct {
	ID                 string
	RegistrationNumber string
	FullName           string
	PreviousSchool     string
	PeriodName         string
	TrackName          string
	Kind               string
	TargetGrade        string // kelas tujuan pindahan
	Status             string
	DocsUploaded       int
	DocsRequired       int
	SubmittedAt        *time.Time
}

// List mengembalikan pendaftar sekolah (tanpa draf kecuali difilter),
// terbaru dulu. Paling banyak 500 baris.
func (s *ApplicantService) List(ctx context.Context, schoolID string, f ListFilter) ([]ListRow, error) {
	q := strings.TrimSpace(f.Query)
	rows, err := s.db.Query(ctx, `
		SELECT a.id, coalesce(a.registration_number, ''), a.full_name, coalesce(a.profile->>'previous_school', ''),
		       p.name, t.name, a.kind, coalesce(a.transfer->>'target_grade', ''), a.status,
		       (SELECT count(*) FROM applicant_documents d WHERE d.applicant_id = a.id AND d.doc_type = ANY (p.required_docs)),
		       cardinality(p.required_docs), a.submitted_at
		FROM applicants a
		JOIN admission_periods p ON p.id = a.period_id
		JOIN admission_tracks t ON t.id = a.track_id
		WHERE a.school_id = $1
		  AND ($2::uuid IS NULL OR a.period_id = $2::uuid)
		  AND ($3::uuid IS NULL OR a.track_id = $3::uuid)
		  AND (CASE WHEN $4 = '' THEN a.status <> 'draf' ELSE a.status = $4 END)
		  AND ($5 = '' OR a.full_name ILIKE '%' || $6 || '%' OR a.registration_number ILIKE $6 || '%')
		  AND ($7 = '' OR a.kind = $7)
		ORDER BY a.submitted_at DESC NULLS LAST, a.created_at DESC
		LIMIT 500`,
		schoolID, uuidOrNil(f.PeriodID), uuidOrNil(f.TrackID), f.Status, q, escapeLike(q), f.Kind)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ListRow, error) {
		var x ListRow
		return x, r.Scan(&x.ID, &x.RegistrationNumber, &x.FullName, &x.PreviousSchool, &x.PeriodName, &x.TrackName, &x.Kind, &x.TargetGrade, &x.Status,
			&x.DocsUploaded, &x.DocsRequired, &x.SubmittedAt)
	})
	if err != nil {
		return nil, mapApplicantError(err)
	}
	return list, nil
}

func uuidOrNil(s string) any {
	if !database.IsUUID(s) {
		return nil
	}
	return s
}

func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
