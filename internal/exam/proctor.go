package exam

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
)

// Layar pengawas (T143): token sesi berputar, aksi pengawas per peserta dan
// per sesi, catatan kejadian, dan berita acara. Hak akses (pengawas sesi
// dalam jendela waktu, koordinator, admin_tu; kepala baca) diperiksa
// handler lewat ProctorLevel.

// Rotasi token sesi.
const (
	TokenRotation = 15 * time.Minute
	// TokenGrace: token sebelumnya masih diterima sebentar setelah berganti
	// (peserta sedang mengetik saat token berganti).
	TokenGrace  = 2 * time.Minute
	tokenLength = 6
)

// SubmitByProctor: dikumpulkan pengawas (kumpul paksa atau tutup sesi).
const SubmitByProctor = "pengawas"

// Error layar pengawas.
var (
	// ErrSessionNotEnded: berita acara baru bisa ditandatangani setelah
	// sesi berakhir atau ditutup.
	ErrSessionNotEnded = errors.New("exam: session not ended")
	// ErrBadPassword: konfirmasi sandi tanda tangan salah.
	ErrBadPassword = errors.New("exam: wrong password")
	// ErrBadNote: catatan kosong atau terlalu panjang.
	ErrBadNote = errors.New("exam: invalid note")
)

func newSessionToken() (string, error) {
	var b strings.Builder
	n := big.NewInt(int64(len(CodeAlphabet)))
	for range tokenLength {
		i, err := rand.Int(rand.Reader, n)
		if err != nil {
			return "", fmt.Errorf("exam: generate token: %w", err)
		}
		b.WriteByte(CodeAlphabet[i.Int64()])
	}
	return b.String(), nil
}

// tokenMatches memeriksa token ketikan peserta terhadap token sesi, atau
// token sebelumnya bila baru berganti kurang dari TokenGrace.
func tokenMatches(given string, token, prev *string, rotated *time.Time, now time.Time) bool {
	if given == "" {
		return false
	}
	eq := func(t *string) bool { return t != nil && subtle.ConstantTimeCompare([]byte(given), []byte(*t)) == 1 }
	if eq(token) {
		return true
	}
	return rotated != nil && now.Sub(*rotated) < TokenGrace && eq(prev)
}

// SessionToken adalah token sesi untuk ditampilkan pengawas.
type SessionToken struct {
	Required bool
	Token    string
	NextAt   time.Time // token berganti pada waktu ini
}

// EnsureToken mengembalikan token sesi dan memutarnya bila sudah berumur
// TokenRotation. Rotasi terjadi saat layar pengawas memuat data (tiap 10
// detik), jadi tidak butuh job.
func EnsureToken(ctx context.Context, db database.DBTX, schoolID, sessionID string, now time.Time) (SessionToken, error) {
	var st SessionToken
	var token *string
	var rotated *time.Time
	err := db.QueryRow(ctx, `SELECT token_required, token, token_rotated_at FROM exam_sessions WHERE school_id = $1 AND id = $2`,
		schoolID, sessionID).Scan(&st.Required, &token, &rotated)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, ErrNotFound
	}
	if err != nil {
		return st, fmt.Errorf("exam: load token: %w", err)
	}
	if !st.Required {
		return st, nil
	}
	if token == nil || rotated == nil || !now.Before(rotated.Add(TokenRotation)) {
		next, err := newSessionToken()
		if err != nil {
			return st, err
		}
		// Bila layar pengawas lain lebih dulu memutar, pakai miliknya.
		err = db.QueryRow(ctx, `UPDATE exam_sessions SET token_prev = token, token = $3, token_rotated_at = $4
			WHERE school_id = $1 AND id = $2 AND (token_rotated_at IS NULL OR token_rotated_at <= $5)
			RETURNING token, token_rotated_at`, schoolID, sessionID, next, now, now.Add(-TokenRotation)).Scan(&token, &rotated)
		if errors.Is(err, pgx.ErrNoRows) {
			err = db.QueryRow(ctx, `SELECT token, token_rotated_at FROM exam_sessions WHERE id = $1`, sessionID).Scan(&token, &rotated)
		}
		if err != nil {
			return st, fmt.Errorf("exam: rotate token: %w", err)
		}
	}
	st.Token, st.NextAt = *token, rotated.Add(TokenRotation)
	return st, nil
}

// SetTokenRequired mewajibkan atau melepas token sesi.
func SetTokenRequired(ctx context.Context, db database.DB, sa StaffActor, sessionID string, on bool, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := sessionForTimer(ctx, tx, sa.SchoolID, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET token_required = $2 WHERE id = $1`, sessionID, on); err != nil {
			return fmt.Errorf("exam: set token required: %w", err)
		}
		if on {
			// Token baru langsung dibuat.
			if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET token_rotated_at = NULL WHERE id = $1`, sessionID); err != nil {
				return fmt.Errorf("exam: reset token: %w", err)
			}
			if _, err := EnsureToken(ctx, tx, sa.SchoolID, sessionID, now); err != nil {
				return err
			}
		}
		return recordAudit(ctx, tx, sa, "exam.session_token", "exam_session", sessionID, nil, map[string]any{"required": on})
	})
}

// ForceSubmit mengumpulkan attempt peserta oleh pengawas. Jawaban yang
// sudah tersimpan menjadi jawaban akhir.
func ForceSubmit(ctx context.Context, db database.DB, sa StaffActor, attemptID string, now time.Time) error {
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		a, err := attemptForTimer(ctx, tx, sa.SchoolID, attemptID, time.Time{})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_attempts SET status = 'dikumpulkan', submitted_at = $2, submit_reason = 'pengawas', paused_at = NULL
			WHERE id = $1`, a.ID, now); err != nil {
			return fmt.Errorf("exam: force submit: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.attempt_submit", "exam_attempt", a.ID, nil,
			map[string]any{"reason": SubmitByProctor, "session_id": a.SessionID})
	})
}

// CloseSession menutup sesi: attempt yang masih berjalan dikumpulkan dan
// peserta tidak bisa mulai lagi. Mengembalikan jumlah attempt yang
// dikumpulkan.
func CloseSession(ctx context.Context, db database.DB, sa StaffActor, sessionID string, now time.Time) (int, error) {
	var n int
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := sessionForTimer(ctx, tx, sa.SchoolID, sessionID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET closed_at = coalesce(closed_at, $2), closed_by = coalesce(closed_by, $3),
			paused_at = NULL WHERE id = $1`, sessionID, now, sa.UserID); err != nil {
			return fmt.Errorf("exam: close session: %w", err)
		}
		tag, err := tx.Exec(ctx, `UPDATE exam_attempts SET status = 'dikumpulkan', submitted_at = $3, submit_reason = 'pengawas', paused_at = NULL
			WHERE school_id = $1 AND session_id = $2 AND status = 'berjalan'`, sa.SchoolID, sessionID, now)
		if err != nil {
			return fmt.Errorf("exam: submit running attempts: %w", err)
		}
		n = int(tag.RowsAffected())
		return recordAudit(ctx, tx, sa, "exam.session_close", "exam_session", sessionID, nil, map[string]any{"submitted": n})
	})
	return n, err
}

// ResetLogin membuat kode masuk baru untuk peserta sesi (lupa kode, kartu
// hilang, atau terkunci karena salah kode). Kode baru hanya berlaku untuk
// sesi ini, tidak terikat perangkat, dan dikembalikan untuk didiktekan.
// Kartu lama tetap berlaku untuk sesi lain di agenda yang sama.
func ResetLogin(ctx context.Context, db database.DB, is *Issuer, sa StaffActor, participantID string, now time.Time) (string, error) {
	if !database.IsUUID(participantID) {
		return "", ErrNotFound
	}
	var code string
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		var o Owner
		var w CardWindow
		var sid, aid *string
		err := tx.QueryRow(ctx, `SELECT p.student_id::text, p.applicant_id::text, x.id, x.starts_at, x.ends_at
			FROM exam_session_participants p JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
			WHERE p.school_id = $1 AND p.id = $2 FOR UPDATE OF p`, sa.SchoolID, participantID).Scan(&sid, &aid, &w.SessionID, &w.Start, &w.End)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("exam: load participant: %w", err)
		}
		if sid != nil {
			o.StudentID = *sid
		}
		if aid != nil {
			o.ApplicantID = *aid
		}
		if !w.End.After(now) {
			return ErrSessionOver
		}
		if code, err = is.IssueCard(ctx, tx, sa.SchoolID, sa.UserID, o, []CardWindow{w}); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam.participant_reset_login", "exam_session_participant", participantID, nil,
			map[string]any{"session_id": w.SessionID})
	})
	return code, err
}

// credentialFor mengembalikan kredensial aktif peserta untuk sesinya.
func credentialFor(ctx context.Context, q database.DBTX, schoolID, participantID string) (string, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT c.id FROM exam_session_participants p
		JOIN student_credentials c ON c.exam_session_id = p.session_id AND c.revoked_at IS NULL AND c.kind = 'kode_ujian'
			AND c.student_id IS NOT DISTINCT FROM p.student_id AND c.applicant_id IS NOT DISTINCT FROM p.applicant_id
		WHERE p.school_id = $1 AND p.id = $2 ORDER BY c.created_at DESC LIMIT 1`, schoolID, participantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", fmt.Errorf("exam: participant credential: %w", err)
	}
	return id, nil
}

// AllowDeviceMove mengizinkan peserta pindah perangkat: ikatan kartu
// dilepas dan attempt berjalan berpindah ke perangkat berikutnya (T141).
func AllowDeviceMove(ctx context.Context, db database.DB, sa StaffActor, participantID string) error {
	cred, err := credentialFor(ctx, db, sa.SchoolID, participantID)
	if err != nil {
		return err
	}
	return ResetDevice(ctx, db, sa.SchoolID, sa.UserID, cred)
}

// AddNote mencatat kejadian sesi (participantID kosong) atau peserta untuk
// berita acara.
func AddNote(ctx context.Context, db database.DB, sa StaffActor, sessionID, participantID, body string) error {
	body = strings.TrimSpace(body)
	if body == "" || len([]rune(body)) > 500 {
		return ErrBadNote
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := sessionForTimer(ctx, tx, sa.SchoolID, sessionID); err != nil {
			return err
		}
		if participantID != "" {
			var ok bool
			if !database.IsUUID(participantID) {
				return ErrNotFound
			}
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM exam_session_participants WHERE school_id = $1 AND id = $2 AND session_id = $3)`,
				sa.SchoolID, participantID, sessionID).Scan(&ok); err != nil {
				return fmt.Errorf("exam: check participant: %w", err)
			}
			if !ok {
				return ErrNotFound
			}
		}
		var id int64
		if err := tx.QueryRow(ctx, `INSERT INTO exam_session_notes (school_id, session_id, participant_id, body, created_by)
			VALUES ($1, $2, $3, $4, $5) RETURNING id`, sa.SchoolID, sessionID, nilIfEmpty(participantID), body, sa.UserID).Scan(&id); err != nil {
			return fmt.Errorf("exam: add note: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.session_note", "exam_session", sessionID, nil,
			map[string]any{"participant_id": participantID, "note": body})
	})
}

// MinutesData adalah isi berita acara satu sesi yang terisi otomatis.
type MinutesData struct {
	SessionAttendance
	Notes     string   // catatan pengawas saat menandatangani
	Incidents []string // catatan kejadian dan kejadian integritas
	SignedAt  *time.Time
	SignedBy  string // nama
}

// LoadMinutes menyusun berita acara sesi: hadir (punya attempt), tidak
// hadir, kejadian (catatan pengawas, keluar halaman berulang, pindah
// perangkat, kumpul paksa, tutup sesi), dan tanda tangan.
func LoadMinutes(ctx context.Context, q database.DBTX, schoolID, sessionID string, loc *time.Location) (MinutesData, error) {
	var m MinutesData
	var signer *string
	err := q.QueryRow(ctx, `SELECT x.minutes_notes, x.minutes_signed_at, u.full_name
		FROM exam_sessions x LEFT JOIN users u ON u.id = x.minutes_signed_by
		WHERE x.school_id = $1 AND x.id = $2`, schoolID, sessionID).Scan(&m.Notes, &m.SignedAt, &signer)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, fmt.Errorf("exam: load minutes: %w", err)
	}
	if signer != nil {
		m.SignedBy = *signer
	}
	rows, err := q.Query(ctx, `
		SELECT coalesce(nullif(p.participant_no, ''), s.nis, ap.registration_number, ''), coalesce(s.full_name, ap.full_name, ''),
			t.id IS NOT NULL, coalesce(t.leave_count, 0), coalesce(t.away_ms, 0), coalesce(t.device_moves, 0), coalesce(t.submit_reason, '')
		FROM exam_session_participants p
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants ap ON ap.id = p.applicant_id
		LEFT JOIN LATERAL (SELECT * FROM exam_attempts t WHERE t.participant_id = p.id ORDER BY t.attempt_no DESC LIMIT 1) t ON true
		WHERE p.school_id = $1 AND p.session_id = $2
		ORDER BY p.seat NULLS LAST, p.participant_no, 2`, schoolID, sessionID)
	if err != nil {
		return m, fmt.Errorf("exam: minutes attendance: %w", err)
	}
	var events []string
	for rows.Next() {
		var no, name, reason string
		var present bool
		var leaves, moves int
		var away int64
		if err := rows.Scan(&no, &name, &present, &leaves, &away, &moves, &reason); err != nil {
			rows.Close()
			return m, fmt.Errorf("exam: minutes attendance: %w", err)
		}
		if !present {
			m.Absent++
			m.AbsentNumbers = append(m.AbsentNumbers, no)
			continue
		}
		m.Present++
		who := strings.TrimSpace(no + " " + name)
		if leaves >= 3 {
			events = append(events, fmt.Sprintf("%s keluar halaman %d kali (%s).", who, leaves, awayLabel(time.Duration(away)*time.Millisecond)))
		}
		if moves > 0 {
			events = append(events, fmt.Sprintf("%s pindah perangkat %d kali.", who, moves))
		}
		if reason == SubmitByProctor {
			events = append(events, who+" dikumpulkan oleh pengawas.")
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return m, fmt.Errorf("exam: minutes attendance: %w", err)
	}
	notes, err := q.Query(ctx, `SELECT n.created_at, n.body, coalesce(s.full_name, ap.full_name, '')
		FROM exam_session_notes n
		LEFT JOIN exam_session_participants p ON p.id = n.participant_id
		LEFT JOIN students s ON s.id = p.student_id
		LEFT JOIN applicants ap ON ap.id = p.applicant_id
		WHERE n.school_id = $1 AND n.session_id = $2 ORDER BY n.id`, schoolID, sessionID)
	if err != nil {
		return m, fmt.Errorf("exam: minutes notes: %w", err)
	}
	for notes.Next() {
		var at time.Time
		var body, name string
		if err := notes.Scan(&at, &body, &name); err != nil {
			notes.Close()
			return m, fmt.Errorf("exam: minutes notes: %w", err)
		}
		line := at.In(loc).Format("15.04") + " "
		if name != "" {
			line += name + ": "
		}
		m.Incidents = append(m.Incidents, line+body)
	}
	notes.Close()
	if err := notes.Err(); err != nil {
		return m, fmt.Errorf("exam: minutes notes: %w", err)
	}
	m.Incidents = append(m.Incidents, events...)
	m.SessionAttendance.Notes = append([]string(nil), m.Incidents...)
	if m.Notes != "" {
		m.SessionAttendance.Notes = append(m.SessionAttendance.Notes, m.Notes)
	}
	return m, nil
}

func awayLabel(d time.Duration) string {
	s := int(d.Round(time.Second).Seconds())
	if s < 60 {
		return fmt.Sprintf("%d detik", s)
	}
	if s%60 == 0 {
		return fmt.Sprintf("%d menit", s/60)
	}
	return fmt.Sprintf("%d menit %d detik", s/60, s%60)
}

// SignMinutes menandatangani berita acara sesi yang sudah berakhir atau
// ditutup: catatan pengawas disimpan dan penanda tangan dikonfirmasi
// dengan sandi akunnya. Menandatangani ulang menimpa catatan dan waktu.
func SignMinutes(ctx context.Context, db database.DB, sa StaffActor, sessionID, notes, password string, now time.Time) error {
	notes = strings.TrimSpace(notes)
	if len([]rune(notes)) > 4000 {
		return ErrBadNote
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := sessionForTimer(ctx, tx, sa.SchoolID, sessionID); err != nil {
			return err
		}
		var ended bool
		if err := tx.QueryRow(ctx, `SELECT closed_at IS NOT NULL OR ends_at <= $2 FROM exam_sessions WHERE id = $1`, sessionID, now).Scan(&ended); err != nil {
			return fmt.Errorf("exam: session ended: %w", err)
		}
		if !ended {
			return ErrSessionNotEnded
		}
		var hash string
		if err := tx.QueryRow(ctx, `SELECT password_hash FROM users WHERE school_id = $1 AND id = $2 AND is_active`, sa.SchoolID, sa.UserID).Scan(&hash); err != nil {
			return ErrBadPassword
		}
		if ok, err := auth.VerifyPassword(password, hash); err != nil || !ok {
			return ErrBadPassword
		}
		if _, err := tx.Exec(ctx, `UPDATE exam_sessions SET minutes_notes = $2, minutes_signed_at = $3, minutes_signed_by = $4 WHERE id = $1`,
			sessionID, notes, now, sa.UserID); err != nil {
			return fmt.Errorf("exam: sign minutes: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam.minutes_sign", "exam_session", sessionID, nil, map[string]any{"notes": notes != ""})
	})
}
