package exam

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/platform/database"
)

// Kode ujian (T124): 8 karakter dari alfabet tanpa huruf/angka mirip
// (tanpa 0/O, 1/I/L).
const (
	CodeAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"
	CodeLength   = 8
	// CodeEarly: kode berlaku sejak 30 menit sebelum sesi.
	CodeEarly = 30 * time.Minute
	// MaxFailures: setelah 5 kali kode salah, peserta terkunci sampai
	// pengawas membuka.
	MaxFailures = 5
)

// lockedForever adalah nilai locked_until untuk "terkunci sampai pengawas
// membuka" (Unlock mengosongkannya).
var lockedForever = time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)

// CodeParams adalah parameter argon2id kode ujian. Lebih ringan dari
// password staf karena ratusan peserta masuk dalam beberapa menit; kode
// berumur pendek, terkunci setelah 5 kali salah, dan dibatasi per IP.
var CodeParams = auth.PasswordParams{MemoryKiB: 4096, Iterations: 3, Parallelism: 1}

// GenerateCode membuat kode ujian acak.
func GenerateCode() (string, error) {
	var b strings.Builder
	max := big.NewInt(int64(len(CodeAlphabet)))
	for range CodeLength {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			return "", fmt.Errorf("exam: generate code: %w", err)
		}
		b.WriteByte(CodeAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// NormalizeCode menyeragamkan kode ketikan peserta: huruf besar, tanpa
// spasi dan tanda hubung.
func NormalizeCode(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == ' ' || r == '-' || r == '\t':
			return -1
		case r >= 'a' && r <= 'z':
			return r - 'a' + 'A'
		}
		return r
	}, strings.TrimSpace(s))
}

// Owner adalah pemilik kredensial: tepat satu dari StudentID atau
// ApplicantID.
type Owner struct {
	StudentID   string
	ApplicantID string
}

func (o Owner) valid() bool { return (o.StudentID == "") != (o.ApplicantID == "") }

// Credential adalah satu kode ujian (tanpa rahasia).
type Credential struct {
	ID            string
	Owner         Owner
	SessionID     string
	ValidFrom     time.Time
	ValidUntil    time.Time
	ResultsUntil  *time.Time
	DeviceID      string
	FailedCount   int
	LockedUntil   *time.Time
	RevokedAt     *time.Time
	secretHash    string
	SessionStarts time.Time // ValidFrom + CodeEarly
}

// IssueRequest adalah permintaan pembuatan kode ujian untuk satu peserta
// dan satu sesi. Start dan End adalah jam sesi ujian.
type IssueRequest struct {
	Owner      Owner
	SessionID  string // kosong sampai tabel sesi ada (T134)
	Start, End time.Time
}

// Issuer membuat kode ujian.
type Issuer struct {
	hasher *auth.Hasher
}

// NewIssuer mengembalikan Issuer dengan CodeParams.
func NewIssuer() *Issuer { return &Issuer{hasher: auth.NewHasher(CodeParams)} }

// Issue membuat kode ujian baru dan mengembalikan teks kodenya. Teks kode
// hanya ada di hasil ini (untuk dicetak di kartu); yang tersimpan hanya
// hash. Kode lama peserta untuk sesi yang sama dicabut.
func (is *Issuer) Issue(ctx context.Context, q database.DBTX, schoolID, actorID string, req IssueRequest) (Credential, string, error) {
	if !req.Owner.valid() {
		return Credential{}, "", errors.New("exam: credential owner must be exactly one of student or applicant")
	}
	if !req.End.After(req.Start) {
		return Credential{}, "", errors.New("exam: session end must be after start")
	}
	code, err := GenerateCode()
	if err != nil {
		return Credential{}, "", err
	}
	hash, err := is.hasher.HashPassword(code)
	if err != nil {
		return Credential{}, "", err
	}
	if req.SessionID != "" {
		if _, err := q.Exec(ctx, `UPDATE student_credentials SET revoked_at = now()
			WHERE school_id = $1 AND kind = 'kode_ujian' AND exam_session_id = $2 AND revoked_at IS NULL
			  AND student_id IS NOT DISTINCT FROM $3 AND applicant_id IS NOT DISTINCT FROM $4`,
			schoolID, req.SessionID, nilIfEmpty(req.Owner.StudentID), nilIfEmpty(req.Owner.ApplicantID)); err != nil {
			return Credential{}, "", fmt.Errorf("exam: revoke old code: %w", err)
		}
	}
	c := Credential{Owner: req.Owner, SessionID: req.SessionID, ValidFrom: req.Start.Add(-CodeEarly), ValidUntil: req.End}
	err = q.QueryRow(ctx, `
		INSERT INTO student_credentials (school_id, student_id, applicant_id, kind, secret_hash, valid_from, valid_until, exam_session_id, created_by)
		VALUES ($1, $2, $3, 'kode_ujian', $4, $5, $6, $7, $8) RETURNING id`,
		schoolID, nilIfEmpty(req.Owner.StudentID), nilIfEmpty(req.Owner.ApplicantID), hash, c.ValidFrom, c.ValidUntil,
		nilIfEmpty(req.SessionID), actorID).Scan(&c.ID)
	if err != nil {
		return Credential{}, "", fmt.Errorf("exam: issue code: %w", err)
	}
	c.SessionStarts = req.Start
	return c, code, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Error login peserta. Pesan untuk peserta dibuat handler.
var (
	// ErrBadLogin: nomor tidak dikenal atau kode salah. Pesannya sama
	// untuk keduanya.
	ErrBadLogin = errors.New("exam: invalid participant login")
	// ErrLocked: terlalu banyak kode salah; pengawas harus membuka.
	ErrLocked = errors.New("exam: participant locked")
	// ErrOtherDevice: kode sudah dipakai di perangkat lain.
	ErrOtherDevice = errors.New("exam: code bound to another device")
)

// NotYetValidError: kode benar tetapi belum berlaku.
type NotYetValidError struct{ ValidFrom, SessionStart time.Time }

func (e *NotYetValidError) Error() string { return "exam: code not yet valid" }

// ExpiredError: kode benar tetapi sesi sudah berakhir dan tidak ada masa
// lihat hasil.
type ExpiredError struct{ Ended time.Time }

func (e *ExpiredError) Error() string { return "exam: code expired" }

// LoginRequest adalah isian login peserta.
type LoginRequest struct {
	SchoolID   string
	Identifier string // NIS atau nomor pendaftaran
	Code       string
	DeviceID   string // cookie perangkat
	Now        time.Time
}

// participantRow adalah peserta yang dicari dari NIS/nomor pendaftaran.
type participantRow struct {
	owner Owner
	kind  string
	name  string
}

// findParticipant mencari siswa aktif berdasarkan NIS, lalu pendaftar
// berdasarkan nomor pendaftaran.
func findParticipant(ctx context.Context, tx pgx.Tx, schoolID, identifier string) (participantRow, error) {
	id := strings.TrimSpace(identifier)
	if id == "" || len(id) > 40 {
		return participantRow{}, ErrBadLogin
	}
	var p participantRow
	err := tx.QueryRow(ctx, `SELECT id, full_name FROM students WHERE school_id = $1 AND nis = $2 AND status = 'aktif'`,
		schoolID, id).Scan(&p.owner.StudentID, &p.name)
	if err == nil {
		p.kind = auth.RoleSiswa
		return p, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return participantRow{}, fmt.Errorf("exam: find student: %w", err)
	}
	err = tx.QueryRow(ctx, `SELECT id, full_name FROM applicants WHERE school_id = $1 AND registration_number = upper($2)`,
		schoolID, id).Scan(&p.owner.ApplicantID, &p.name)
	if errors.Is(err, pgx.ErrNoRows) {
		return participantRow{}, ErrBadLogin
	}
	if err != nil {
		return participantRow{}, fmt.Errorf("exam: find applicant: %w", err)
	}
	p.kind = auth.RolePendaftar
	return p, nil
}

// ownerWhere memilih kredensial milik owner ($1 school_id, $2 student_id,
// $3 applicant_id).
const ownerWhere = `school_id = $1 AND student_id IS NOT DISTINCT FROM $2 AND applicant_id IS NOT DISTINCT FROM $3
	AND kind = 'kode_ujian' AND revoked_at IS NULL`

// recentWindow: kode yang berakhir lebih lama dari ini tidak diperiksa lagi.
const recentWindow = 7 * 24 * time.Hour

// Login memeriksa NIS/nomor pendaftaran dan kode ujian lalu mengembalikan
// identitas peserta. Kode benar di dalam jendela mengerjakan mengikat
// perangkat pertama; setelah sesi berakhir, kode hanya membuka hasil
// (ReadOnly) bila masa lihat hasil sudah dibuka.
func Login(ctx context.Context, db database.DB, req LoginRequest) (auth.Participant, error) {
	var out auth.Participant
	// failed diisi bila kode salah. Transaksi tetap di-commit agar hitungan
	// gagal dan penguncian tersimpan; error dikembalikan setelahnya.
	var failed error
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		p, err := findParticipant(ctx, tx, req.SchoolID, req.Identifier)
		if err != nil {
			return err
		}
		sid, aid := nilIfEmpty(p.owner.StudentID), nilIfEmpty(p.owner.ApplicantID)
		rows, err := tx.Query(ctx, `
			SELECT id, secret_hash, valid_from, valid_until, results_until, coalesce(device_id::text, ''), failed_count, locked_until
			FROM student_credentials WHERE `+ownerWhere+`
			  AND coalesce(results_until, valid_until) > $4
			ORDER BY valid_from
			FOR UPDATE`, req.SchoolID, sid, aid, req.Now.Add(-recentWindow))
		if err != nil {
			return fmt.Errorf("exam: load credentials: %w", err)
		}
		creds, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Credential, error) {
			var c Credential
			err := r.Scan(&c.ID, &c.secretHash, &c.ValidFrom, &c.ValidUntil, &c.ResultsUntil, &c.DeviceID, &c.FailedCount, &c.LockedUntil)
			return c, err
		})
		if err != nil {
			return fmt.Errorf("exam: load credentials: %w", err)
		}
		for _, c := range creds {
			if c.LockedUntil != nil && req.Now.Before(*c.LockedUntil) {
				return ErrLocked
			}
		}
		code := NormalizeCode(req.Code)
		var match *Credential
		if len(code) == CodeLength {
			for i := range creds {
				ok, err := auth.VerifyPassword(code, creds[i].secretHash)
				if err != nil {
					return err
				}
				if ok {
					match = &creds[i]
					break
				}
			}
		}
		if match == nil {
			if len(creds) == 0 {
				return ErrBadLogin
			}
			// Hitung gagal di semua kode aktif peserta; kunci di kegagalan ke-5.
			var failures int
			err := tx.QueryRow(ctx, `
				WITH u AS (
					UPDATE student_credentials SET failed_count = failed_count + 1,
					       locked_until = CASE WHEN failed_count + 1 >= $5 THEN $6::timestamptz ELSE locked_until END
					WHERE `+ownerWhere+` AND coalesce(results_until, valid_until) > $4
					RETURNING failed_count)
				SELECT coalesce(max(failed_count), 0) FROM u`, req.SchoolID, sid, aid, req.Now.Add(-recentWindow), MaxFailures, lockedForever).Scan(&failures)
			if err != nil {
				return fmt.Errorf("exam: count failure: %w", err)
			}
			failed = ErrBadLogin
			if failures >= MaxFailures {
				failed = ErrLocked
			}
			return nil
		}
		if _, err := tx.Exec(ctx, `UPDATE student_credentials SET failed_count = 0 WHERE `+ownerWhere+` AND failed_count > 0`, req.SchoolID, sid, aid); err != nil {
			return fmt.Errorf("exam: reset failures: %w", err)
		}
		if req.Now.Before(match.ValidFrom) {
			return &NotYetValidError{ValidFrom: match.ValidFrom, SessionStart: match.ValidFrom.Add(CodeEarly)}
		}
		out = auth.Participant{Kind: p.kind, SchoolID: req.SchoolID, StudentID: p.owner.StudentID, ApplicantID: p.owner.ApplicantID,
			CredentialID: match.ID, Name: p.name}
		switch {
		case !req.Now.After(match.ValidUntil):
			// Jendela mengerjakan: ikat ke perangkat pertama.
			if req.DeviceID == "" {
				return ErrOtherDevice
			}
			if match.DeviceID != "" && match.DeviceID != req.DeviceID {
				return ErrOtherDevice
			}
			if match.DeviceID == "" {
				if _, err := tx.Exec(ctx, `UPDATE student_credentials SET device_id = $2 WHERE id = $1`, match.ID, req.DeviceID); err != nil {
					return fmt.Errorf("exam: bind device: %w", err)
				}
			}
			out.ExpiresAt = match.ValidUntil.Add(auth.ParticipantGrace)
		case match.ResultsUntil != nil && !req.Now.After(*match.ResultsUntil):
			// Setelah sesi: hanya lihat hasil, dari perangkat mana pun.
			out.ReadOnly = true
			out.ExpiresAt = req.Now.Add(2 * time.Hour)
			if match.ResultsUntil.Before(out.ExpiresAt) {
				out.ExpiresAt = *match.ResultsUntil
			}
		default:
			return &ExpiredError{Ended: match.ValidUntil}
		}
		return nil
	})
	if err != nil {
		return auth.Participant{}, err
	}
	if failed != nil {
		return auth.Participant{}, failed
	}
	return out, nil
}

// CanAttempt melaporkan apakah peserta boleh memulai atau mengubah attempt.
// Sesi setelah ujian berakhir hanya untuk membaca hasil.
func CanAttempt(p auth.Participant) bool { return !p.ReadOnly }

// Unlock membuka kunci peserta pemilik kredensial credentialID (oleh
// pengawas atau admin) dan mencatat audit. Hitungan gagal direset.
func Unlock(ctx context.Context, db database.DB, schoolID, actorID, credentialID string) error {
	return ownerUpdate(ctx, db, schoolID, actorID, credentialID, "participant.unlock",
		`failed_count = 0, locked_until = NULL`)
}

// ResetDevice melepas ikatan perangkat kode credentialID agar peserta bisa
// masuk dari perangkat lain (mis. HP mati), dan mencatat audit.
func ResetDevice(ctx context.Context, db database.DB, schoolID, actorID, credentialID string) error {
	if !database.IsUUID(credentialID) {
		return ErrBadLogin
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `UPDATE student_credentials SET device_id = NULL WHERE school_id = $1 AND id = $2 AND revoked_at IS NULL`,
			schoolID, credentialID)
		if err != nil {
			return fmt.Errorf("exam: reset device: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return ErrBadLogin
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: schoolID, UserID: actorID, Action: "participant.reset_device",
			EntityType: "student_credential", EntityID: credentialID})
	})
}

func ownerUpdate(ctx context.Context, db database.DB, schoolID, actorID, credentialID, action, set string) error {
	if !database.IsUUID(credentialID) {
		return ErrBadLogin
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var sid, aid *string
		err := tx.QueryRow(ctx, `SELECT student_id, applicant_id FROM student_credentials WHERE school_id = $1 AND id = $2`,
			schoolID, credentialID).Scan(&sid, &aid)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrBadLogin
		}
		if err != nil {
			return fmt.Errorf("exam: %s: %w", action, err)
		}
		if _, err := tx.Exec(ctx, `UPDATE student_credentials SET `+set+` WHERE `+ownerWhere, schoolID, sid, aid); err != nil {
			return fmt.Errorf("exam: %s: %w", action, err)
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: schoolID, UserID: actorID, Action: action,
			EntityType: "student_credential", EntityID: credentialID})
	})
}
