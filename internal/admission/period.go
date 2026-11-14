// Package admission mengelola pendaftaran siswa baru (SPMB sekolah): periode
// dan jalur (T89), pendaftar, verifikasi, tes seleksi, dan penerimaan.
//
// Data pendaftar adalah data pribadi anak: jangan tulis nama, NIK, atau
// nomor HP ke log.
package admission

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/money"
	"akra/internal/school"
)

// Status periode.
const (
	StatusDraft  = "draf"
	StatusOpen   = "buka"
	StatusClosed = "tutup"
)

// Jenis periode dan pendaftar (T109).
const (
	KindNew      = "baru"
	KindTransfer = "pindahan"
)

// KindLabel mengembalikan nama jenis pendaftaran untuk tampilan.
func KindLabel(kind string) string {
	if kind == KindTransfer {
		return "Siswa pindahan"
	}
	return "Siswa baru"
}

// Aksi audit periode dan jalur.
const (
	ActionPeriodCreate = "admission.period_create"
	ActionPeriodUpdate = "admission.period_update"
	ActionPeriodStatus = "admission.period_status"
	ActionPeriodDelete = "admission.period_delete"
	ActionTrackCreate  = "admission.track_create"
	ActionTrackUpdate  = "admission.track_update"
	ActionTrackDelete  = "admission.track_delete"
)

// Docs adalah dokumen persyaratan yang bisa diminta, urut tampil.
var Docs = []struct{ Key, Label string }{
	{"kk", "Kartu Keluarga"},
	{"akta", "Akta kelahiran"},
	{"ijazah", "Ijazah/SKL"},
	{"foto", "Pas foto"},
	{"kip", "Kartu Indonesia Pintar"},
	{"surat_pindah", "Surat keterangan pindah"},
	{"rapor", "Rapor terakhir"},
}

const dateLayout = "2006-01-02"

var (
	// ErrNotFound dikembalikan jika periode atau jalur tidak ada atau milik
	// sekolah lain.
	ErrNotFound = errors.New("admission: not found")
	// ErrNoTracks dikembalikan saat membuka periode yang belum punya jalur.
	ErrNoTracks = errors.New("admission: period has no tracks")
	// ErrInUse dikembalikan saat menghapus periode/jalur yang sudah dipakai.
	ErrInUse = errors.New("admission: period or track is in use")
)

// ValidationError berisi pesan per field untuk ditampilkan di form.
type ValidationError struct {
	Fields map[string]string
}

func (e *ValidationError) Error() string {
	keys := make([]string, 0, len(e.Fields))
	for k := range e.Fields {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return "admission: invalid input: " + strings.Join(keys, ", ")
}

func fieldErr(field, msg string) *ValidationError {
	return &ValidationError{Fields: map[string]string{field: msg}}
}

// Period adalah satu gelombang pendaftaran.
type Period struct {
	ID                      string
	SchoolID                string
	AcademicYearID          string
	AcademicYearName        string
	Name                    string
	Kind                    string
	TargetGrade             int // 0 untuk periode pindahan (tingkat dipilih pendaftar)
	OpensOn, ClosesOn       time.Time
	FeeAmount               int64
	FeeRequiredBeforeVerify bool
	RequiredDocs            []string
	Status                  string
	Tracks                  []Track
	TrackCount              int
	CreatedAt               time.Time
}

// IsOpen melaporkan apakah pendaftaran diterima pada hari today.
func (p Period) IsOpen(today time.Time) bool {
	d := today.Format(dateLayout)
	return p.Status == StatusOpen && d >= p.OpensOn.Format(dateLayout) && d <= p.ClosesOn.Format(dateLayout)
}

// Requires melaporkan apakah dokumen key diminta.
func (p Period) Requires(key string) bool { return slices.Contains(p.RequiredDocs, key) }

// Track adalah satu jalur pendaftaran di sebuah periode.
type Track struct {
	ID        string
	PeriodID  string
	Name      string
	Quota     int // 0 = tanpa batas
	UsesTest  bool
	SortOrder int
}

// PeriodForm adalah isian form periode apa adanya.
type PeriodForm struct {
	AcademicYearID string
	Name           string
	Kind           string // hanya dipakai saat membuat; jenis tidak bisa diubah
	TargetGrade    string
	OpensOn        string
	ClosesOn       string
	FeeAmount      string
	FeeRequired    bool
	RequiredDocs   []string
}

// TrackForm adalah isian form jalur apa adanya.
type TrackForm struct {
	Name     string
	Quota    string
	UsesTest bool
}

// Service menjalankan aturan bisnis periode dan jalur.
type Service struct {
	db    database.DB
	today func() time.Time
}

// NewService mengembalikan Service.
func NewService(db database.DB) *Service {
	return &Service{db: db, today: clock.Today}
}

// Today mengembalikan tanggal hari ini (WIB) yang dipakai service.
func (s *Service) Today() time.Time { return s.today() }

const periodColumns = `p.id, p.school_id, p.academic_year_id, y.name, p.name, p.kind, coalesce(p.target_grade, 0), p.opens_on, p.closes_on,
	p.fee_amount, p.fee_required_before_verify, p.required_docs, p.status, p.created_at,
	(SELECT count(*) FROM admission_tracks t WHERE t.period_id = p.id)`

func scanPeriod(row pgx.CollectableRow) (Period, error) {
	var p Period
	var grade int16
	err := row.Scan(&p.ID, &p.SchoolID, &p.AcademicYearID, &p.AcademicYearName, &p.Name, &p.Kind, &grade, &p.OpensOn, &p.ClosesOn,
		&p.FeeAmount, &p.FeeRequiredBeforeVerify, &p.RequiredDocs, &p.Status, &p.CreatedAt, &p.TrackCount)
	p.TargetGrade = int(grade)
	return p, err
}

// List mengembalikan semua periode sekolah, terbaru dulu.
func (s *Service) List(ctx context.Context, schoolID string) ([]Period, error) {
	rows, err := s.db.Query(ctx, `SELECT `+periodColumns+`
		FROM admission_periods p JOIN academic_years y ON y.id = p.academic_year_id AND y.school_id = p.school_id
		WHERE p.school_id = $1 ORDER BY p.opens_on DESC, p.name`, schoolID)
	if err != nil {
		return nil, mapError("list periods", err)
	}
	list, err := pgx.CollectRows(rows, scanPeriod)
	if err != nil {
		return nil, mapError("list periods", err)
	}
	return list, nil
}

// Get mengembalikan satu periode beserta jalurnya.
func (s *Service) Get(ctx context.Context, schoolID, id string) (Period, error) {
	return getPeriod(ctx, s.db, schoolID, id, false)
}

func getPeriod(ctx context.Context, q database.DBTX, schoolID, id string, forUpdate bool) (Period, error) {
	if !database.IsUUID(id) {
		return Period{}, ErrNotFound
	}
	sql := `SELECT ` + periodColumns + `
		FROM admission_periods p JOIN academic_years y ON y.id = p.academic_year_id AND y.school_id = p.school_id
		WHERE p.school_id = $1 AND p.id = $2`
	if forUpdate {
		sql += ` FOR UPDATE OF p`
	}
	rows, err := q.Query(ctx, sql, schoolID, id)
	if err != nil {
		return Period{}, mapError("get period", err)
	}
	p, err := pgx.CollectExactlyOneRow(rows, scanPeriod)
	if err != nil {
		return Period{}, mapError("get period", err)
	}
	p.Tracks, err = listTracks(ctx, q, schoolID, p.ID)
	return p, err
}

func listTracks(ctx context.Context, q database.DBTX, schoolID, periodID string) ([]Track, error) {
	rows, err := q.Query(ctx, `SELECT id, period_id, name, coalesce(quota, 0), uses_test, sort_order
		FROM admission_tracks WHERE school_id = $1 AND period_id = $2 ORDER BY sort_order, name`, schoolID, periodID)
	if err != nil {
		return nil, mapError("list tracks", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Track, error) {
		var t Track
		return t, row.Scan(&t.ID, &t.PeriodID, &t.Name, &t.Quota, &t.UsesTest, &t.SortOrder)
	})
	if err != nil {
		return nil, mapError("list tracks", err)
	}
	return list, nil
}

// OpenPeriods mengembalikan periode yang menerima pendaftaran hari ini,
// beserta jalurnya. Dipakai formulir publik (T90).
func (s *Service) OpenPeriods(ctx context.Context, schoolID string) ([]Period, error) {
	all, err := s.List(ctx, schoolID)
	if err != nil {
		return nil, err
	}
	var open []Period
	for _, p := range all {
		if p.IsOpen(s.today()) && p.TrackCount > 0 {
			p.Tracks, err = listTracks(ctx, s.db, schoolID, p.ID)
			if err != nil {
				return nil, err
			}
			open = append(open, p)
		}
	}
	return open, nil
}

type periodInput struct {
	AcademicYearID    string
	Name              string
	Kind              string
	TargetGrade       *int // nil untuk periode pindahan
	OpensOn, ClosesOn time.Time
	FeeAmount         int64
	FeeRequired       bool
	RequiredDocs      []string
}

func validatePeriod(f PeriodForm) (periodInput, error) {
	fields := map[string]string{}
	in := periodInput{
		AcademicYearID: strings.TrimSpace(f.AcademicYearID),
		Name:           strings.Join(strings.Fields(f.Name), " "),
		FeeRequired:    f.FeeRequired,
		Kind:           f.Kind,
	}
	if in.Kind == "" {
		in.Kind = KindNew
	}
	if in.Kind != KindNew && in.Kind != KindTransfer {
		fields["kind"] = "Pilih jenis pendaftaran."
	}
	switch {
	case in.Name == "":
		fields["name"] = "Nama periode wajib diisi."
	case utf8.RuneCountInString(in.Name) > 80:
		fields["name"] = "Nama periode maksimal 80 karakter."
	}
	if !database.IsUUID(in.AcademicYearID) {
		fields["academic_year_id"] = "Pilih tahun ajaran."
	}
	if in.Kind != KindTransfer {
		if g, err := strconv.Atoi(strings.TrimSpace(f.TargetGrade)); err != nil || g < 1 || g > 12 {
			fields["target_grade"] = "Tingkat tujuan harus 1–12."
		} else {
			in.TargetGrade = &g
		}
	}
	var err error
	if in.OpensOn, err = time.Parse(dateLayout, strings.TrimSpace(f.OpensOn)); err != nil {
		fields["opens_on"] = "Tanggal buka tidak valid."
	}
	if in.ClosesOn, err = time.Parse(dateLayout, strings.TrimSpace(f.ClosesOn)); err != nil {
		fields["closes_on"] = "Tanggal tutup tidak valid."
	} else if fields["opens_on"] == "" && in.ClosesOn.Before(in.OpensOn) {
		fields["closes_on"] = "Tanggal tutup tidak boleh sebelum tanggal buka."
	}
	if fee := strings.TrimSpace(f.FeeAmount); fee != "" {
		if in.FeeAmount, err = money.ParseRupiah(fee); err != nil {
			fields["fee_amount"] = "Biaya formulir tidak valid. Contoh: 150.000"
		}
	}
	for _, d := range f.RequiredDocs {
		ok := false
		for _, doc := range Docs {
			ok = ok || doc.Key == d
		}
		if !ok {
			fields["required_docs"] = "Pilih dokumen yang tersedia."
			break
		}
		if !slices.Contains(in.RequiredDocs, d) {
			in.RequiredDocs = append(in.RequiredDocs, d)
		}
	}
	if in.FeeAmount == 0 {
		in.FeeRequired = false
	}
	if len(fields) > 0 {
		return periodInput{}, &ValidationError{Fields: fields}
	}
	if in.RequiredDocs == nil {
		in.RequiredDocs = []string{}
	}
	return in, nil
}

// Create membuat periode berstatus draf.
func (s *Service) Create(ctx context.Context, actor school.Actor, f PeriodForm) (Period, error) {
	in, err := validatePeriod(f)
	if err != nil {
		return Period{}, err
	}
	var created Period
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		var id string
		err := tx.QueryRow(ctx, `
			INSERT INTO admission_periods (school_id, academic_year_id, name, kind, target_grade, opens_on, closes_on,
			       fee_amount, fee_required_before_verify, required_docs, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`,
			actor.SchoolID, in.AcademicYearID, in.Name, in.Kind, in.TargetGrade, in.OpensOn, in.ClosesOn,
			in.FeeAmount, in.FeeRequired, in.RequiredDocs, actor.UserID).Scan(&id)
		if err != nil {
			return mapError("create period", err)
		}
		if created, err = getPeriod(ctx, tx, actor.SchoolID, id, false); err != nil {
			return err
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionPeriodCreate, id, nil, periodSnapshot(created)))
	})
	if err != nil {
		return Period{}, periodFieldError(err)
	}
	return created, nil
}

// Update mengubah periode. Status tidak diubah di sini.
func (s *Service) Update(ctx context.Context, actor school.Actor, id string, f PeriodForm) (Period, error) {
	current, err := s.Get(ctx, actor.SchoolID, id)
	if err != nil {
		return Period{}, err
	}
	f.Kind = current.Kind
	in, err := validatePeriod(f)
	if err != nil {
		return Period{}, err
	}
	var updated Period
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := getPeriod(ctx, tx, actor.SchoolID, id, true)
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE admission_periods SET academic_year_id = $3, name = $4, target_grade = $5, opens_on = $6, closes_on = $7,
			       fee_amount = $8, fee_required_before_verify = $9, required_docs = $10, updated_at = now()
			WHERE school_id = $1 AND id = $2`,
			actor.SchoolID, id, in.AcademicYearID, in.Name, in.TargetGrade, in.OpensOn, in.ClosesOn,
			in.FeeAmount, in.FeeRequired, in.RequiredDocs)
		if err != nil {
			return mapError("update period", err)
		}
		if updated, err = getPeriod(ctx, tx, actor.SchoolID, id, false); err != nil {
			return err
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionPeriodUpdate, id, periodSnapshot(before), periodSnapshot(updated)))
	})
	if err != nil {
		return Period{}, periodFieldError(err)
	}
	return updated, nil
}

// SetStatus mengubah status periode. Periode tanpa jalur tidak bisa dibuka.
func (s *Service) SetStatus(ctx context.Context, actor school.Actor, id, status string) (Period, error) {
	if status != StatusDraft && status != StatusOpen && status != StatusClosed {
		return Period{}, fieldErr("status", "Status tidak dikenal.")
	}
	var updated Period
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		before, err := getPeriod(ctx, tx, actor.SchoolID, id, true)
		if err != nil {
			return err
		}
		if status == StatusOpen && len(before.Tracks) == 0 {
			return ErrNoTracks
		}
		if before.Status == status {
			updated = before
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE admission_periods SET status = $3, updated_at = now() WHERE school_id = $1 AND id = $2`,
			actor.SchoolID, id, status); err != nil {
			return mapError("set period status", err)
		}
		updated = before
		updated.Status = status
		return audit.Record(ctx, tx, periodAudit(actor, ActionPeriodStatus, id,
			map[string]any{"status": before.Status}, map[string]any{"status": status}))
	})
	return updated, err
}

// Delete menghapus periode beserta jalurnya. Hanya periode draf yang bisa
// dihapus; periode yang sudah punya pendaftar ditolak database (ErrInUse).
func (s *Service) Delete(ctx context.Context, actor school.Actor, id string) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		p, err := getPeriod(ctx, tx, actor.SchoolID, id, true)
		if err != nil {
			return err
		}
		if p.Status != StatusDraft {
			return ErrInUse
		}
		if _, err := tx.Exec(ctx, `DELETE FROM admission_tracks WHERE school_id = $1 AND period_id = $2`, actor.SchoolID, id); err != nil {
			return deleteError(mapError("delete tracks", err))
		}
		if _, err := tx.Exec(ctx, `DELETE FROM admission_periods WHERE school_id = $1 AND id = $2`, actor.SchoolID, id); err != nil {
			return deleteError(mapError("delete period", err))
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionPeriodDelete, id, periodSnapshot(p), nil))
	})
}

type trackInput struct {
	Name     string
	Quota    *int
	UsesTest bool
}

func validateTrack(f TrackForm) (trackInput, error) {
	fields := map[string]string{}
	in := trackInput{Name: strings.Join(strings.Fields(f.Name), " "), UsesTest: f.UsesTest}
	switch {
	case in.Name == "":
		fields["track_name"] = "Nama jalur wajib diisi."
	case utf8.RuneCountInString(in.Name) > 60:
		fields["track_name"] = "Nama jalur maksimal 60 karakter."
	}
	if q := strings.TrimSpace(f.Quota); q != "" {
		n, err := strconv.Atoi(q)
		if err != nil || n < 1 || n > 10000 {
			fields["quota"] = "Kuota harus angka 1–10.000, atau kosong untuk tanpa batas."
		} else {
			in.Quota = &n
		}
	}
	if len(fields) > 0 {
		return trackInput{}, &ValidationError{Fields: fields}
	}
	return in, nil
}

// AddTrack menambah jalur ke periode.
func (s *Service) AddTrack(ctx context.Context, actor school.Actor, periodID string, f TrackForm) (Track, error) {
	in, err := validateTrack(f)
	if err != nil {
		return Track{}, err
	}
	var t Track
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `
			INSERT INTO admission_tracks (school_id, period_id, name, quota, uses_test, sort_order)
			VALUES ($1, $2, $3, $4, $5, (SELECT coalesce(max(sort_order), 0) + 1 FROM admission_tracks WHERE period_id = $2))
			RETURNING id, period_id, name, coalesce(quota, 0), uses_test, sort_order`,
			actor.SchoolID, periodID, in.Name, in.Quota, in.UsesTest).
			Scan(&t.ID, &t.PeriodID, &t.Name, &t.Quota, &t.UsesTest, &t.SortOrder)
		if err != nil {
			return mapError("add track", err)
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTrackCreate, periodID, nil, trackSnapshot(t)))
	})
	if err != nil {
		return Track{}, periodFieldError(err)
	}
	return t, nil
}

// UpdateTrack mengubah jalur.
func (s *Service) UpdateTrack(ctx context.Context, actor school.Actor, periodID, trackID string, f TrackForm) (Track, error) {
	in, err := validateTrack(f)
	if err != nil {
		return Track{}, err
	}
	if !database.IsUUID(trackID) {
		return Track{}, ErrNotFound
	}
	var t Track
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		p, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true)
		if err != nil {
			return err
		}
		var before *Track
		for i := range p.Tracks {
			if p.Tracks[i].ID == trackID {
				before = &p.Tracks[i]
			}
		}
		if before == nil {
			return ErrNotFound
		}
		err = tx.QueryRow(ctx, `
			UPDATE admission_tracks SET name = $4, quota = $5, uses_test = $6
			WHERE school_id = $1 AND period_id = $2 AND id = $3
			RETURNING id, period_id, name, coalesce(quota, 0), uses_test, sort_order`,
			actor.SchoolID, periodID, trackID, in.Name, in.Quota, in.UsesTest).
			Scan(&t.ID, &t.PeriodID, &t.Name, &t.Quota, &t.UsesTest, &t.SortOrder)
		if err != nil {
			return mapError("update track", err)
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTrackUpdate, periodID, trackSnapshot(*before), trackSnapshot(t)))
	})
	if err != nil {
		return Track{}, periodFieldError(err)
	}
	return t, nil
}

// DeleteTrack menghapus jalur. Periode yang sedang dibuka harus tetap punya
// minimal satu jalur; jalur yang sudah punya pendaftar ditolak (ErrInUse).
func (s *Service) DeleteTrack(ctx context.Context, actor school.Actor, periodID, trackID string) error {
	if !database.IsUUID(trackID) {
		return ErrNotFound
	}
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		p, err := getPeriod(ctx, tx, actor.SchoolID, periodID, true)
		if err != nil {
			return err
		}
		var found *Track
		for i := range p.Tracks {
			if p.Tracks[i].ID == trackID {
				found = &p.Tracks[i]
			}
		}
		if found == nil {
			return ErrNotFound
		}
		if p.Status == StatusOpen && len(p.Tracks) == 1 {
			return ErrNoTracks
		}
		if _, err := tx.Exec(ctx, `DELETE FROM admission_tracks WHERE school_id = $1 AND id = $2`, actor.SchoolID, trackID); err != nil {
			return deleteError(mapError("delete track", err))
		}
		return audit.Record(ctx, tx, periodAudit(actor, ActionTrackDelete, periodID, trackSnapshot(*found), nil))
	})
}

func periodSnapshot(p Period) map[string]any {
	return map[string]any{
		"name": p.Name, "kind": p.Kind, "academic_year_id": p.AcademicYearID, "target_grade": p.TargetGrade,
		"opens_on": p.OpensOn.Format(dateLayout), "closes_on": p.ClosesOn.Format(dateLayout),
		"fee_amount": p.FeeAmount, "fee_required_before_verify": p.FeeRequiredBeforeVerify,
		"required_docs": p.RequiredDocs, "status": p.Status,
	}
}

func trackSnapshot(t Track) map[string]any {
	return map[string]any{"track_id": t.ID, "name": t.Name, "quota": t.Quota, "uses_test": t.UsesTest}
}

func periodAudit(actor school.Actor, action, id string, before, after any) audit.Entry {
	return audit.Entry{
		SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
		Action: action, EntityType: "admission_period", EntityID: id,
		Before: before, After: after,
	}
}

func periodFieldError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.ConstraintName {
		case "admission_periods_name_key":
			return fieldErr("name", "Nama periode sudah dipakai di tahun ajaran ini.")
		case "admission_periods_year_fkey":
			return fieldErr("academic_year_id", "Pilih tahun ajaran.")
		case "admission_tracks_name_key":
			return fieldErr("track_name", "Nama jalur sudah dipakai di periode ini.")
		}
	}
	return err
}

// mapError mengubah error database menjadi error domain tanpa membawa pesan
// PostgreSQL (yang bisa memuat data baris). Pelanggaran unique dan foreign
// key dikembalikan sebagai *pgconn.PgError yang hanya berisi kode dan nama
// constraint, agar periodFieldError dan deleteError bisa memetakannya.
func mapError(op string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		if pgErr.Code == "23505" || pgErr.Code == "23503" {
			return &pgconn.PgError{Code: pgErr.Code, ConstraintName: pgErr.ConstraintName}
		}
		return fmt.Errorf("admission: %s: database error %s (constraint %q)", op, pgErr.Code, pgErr.ConstraintName)
	}
	return fmt.Errorf("admission: %s: %w", op, err)
}

// deleteError: baris yang masih dirujuk tabel lain (mis. pendaftar) tidak
// bisa dihapus.
func deleteError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23503" {
		return ErrInUse
	}
	return err
}
