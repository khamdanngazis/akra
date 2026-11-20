package exam

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
	"akra/internal/platform/database"
)

// Kartu tes pendaftar (T149). Kode kartu pendaftar diturunkan dari kunci
// server (EXAM_TOKEN_KEYS) dan nonce acak per kartu: kode = HMAC(kunci,
// nonce) dalam alfabet kode. Database hanya menyimpan nonce dan hash kode,
// sehingga kartu yang sama bisa ditampilkan ulang di halaman status dan
// bukti pendaftaran tanpa menyimpan kode, dan tetap cocok dengan kartu yang
// dicetak TU. Saat kunci dirotasi, semua kunci dicoba; kartu yang tidak
// bisa diturunkan lagi diterbitkan ulang.

// cardCodeDomain memisahkan pemakaian kunci untuk kode kartu dari token
// attempt.
const cardCodeDomain = "akra/kartu-pendaftar/v1:"

// cardCode menurunkan kode kartu dari kunci ke-i dan nonce.
func (s *TokenSigner) cardCode(i int, nonce string) string {
	out := make([]byte, 0, CodeLength)
	for round := byte(0); len(out) < CodeLength; round++ {
		m := hmac.New(sha256.New, s.keys[i])
		m.Write([]byte(cardCodeDomain))
		m.Write([]byte(nonce))
		m.Write([]byte{round})
		// Penolakan nilai di atas kelipatan panjang alfabet agar tidak
		// bias.
		limit := byte(256 - 256%len(CodeAlphabet))
		for _, b := range m.Sum(nil) {
			if b < limit && len(out) < CodeLength {
				out = append(out, CodeAlphabet[int(b)%len(CodeAlphabet)])
			}
		}
	}
	return string(out)
}

// WithCardCodes mengembalikan Issuer yang menurunkan kode kartu pendaftar
// dari kunci s (kartu siswa tetap acak).
func (is *Issuer) WithCardCodes(s *TokenSigner) *Issuer {
	c := *is
	c.codes = s
	return &c
}

// newNonce membuat UUID acak (versi 4) sebagai nonce kartu.
func newNonce() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("exam: card nonce: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}

// ApplicantTestCard adalah satu sesi tes online di kartu tes pendaftar.
type ApplicantTestCard struct {
	ExamName   string
	SessionID  string
	Start, End time.Time
	Remote     bool // dari rumah
	Practice   bool // ujian latihan (coba antarmuka)
	Submitted  bool
	Ended      bool
}

// ApplicantCard adalah kartu tes online satu pendaftar.
type ApplicantCard struct {
	Identifier string // nomor pendaftaran (untuk login)
	LoginURL   string
	Code       string // kosong bila belum tersedia
	CodeFrom   time.Time
	Tests      []ApplicantTestCard
}

// ErrNoCardCodes: issuer tanpa kunci kode kartu.
var ErrNoCardCodes = errors.New("exam: card codes need a token signer")

// LoadApplicantCard menyusun kartu tes online pendaftar: sesi tes seleksi
// dan latihan yang ia ikuti (yang berakhir lebih dari 3 hari lalu tidak
// tampil) dan kode aksesnya. Kode tersedia paling cepat CardLead sebelum
// sesi pertama yang belum berakhir; bila belum ada kartu yang bisa
// diturunkan untuk semua sesi itu, kartu baru diterbitkan (kode lama
// pendaftar untuk sesi itu tidak berlaku lagi).
func LoadApplicantCard(ctx context.Context, db database.DB, is *Issuer, schoolID, applicantID, baseURL string, now time.Time) (ApplicantCard, error) {
	var card ApplicantCard
	if is == nil || is.codes == nil {
		return card, ErrNoCardCodes
	}
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// Satu penerbit per pendaftar pada satu waktu.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('applicant-card:' || $1))`, applicantID); err != nil {
			return fmt.Errorf("exam: card lock: %w", err)
		}
		var schoolCode string
		if err := tx.QueryRow(ctx, `SELECT s.code, a.registration_number FROM schools s JOIN applicants a ON a.school_id = s.id
			WHERE s.id = $1 AND a.id = $2`, schoolID, applicantID).Scan(&schoolCode, &card.Identifier); err != nil {
			return fmt.Errorf("exam: card applicant: %w", err)
		}
		card.LoginURL = ParticipantLoginURL(baseURL, schoolCode, card.Identifier)
		rows, err := tx.Query(ctx, `
			SELECT e.name, x.id, x.starts_at, x.ends_at + make_interval(mins => x.extra_minutes), x.location = 'jarak_jauh', e.purpose = 'latihan',
			       EXISTS (SELECT 1 FROM exam_attempts t WHERE t.participant_id = p.id AND t.status = 'dikumpulkan'), e.owner_id
			FROM exam_session_participants p
			JOIN exam_sessions x ON x.id = p.session_id AND x.cancelled_at IS NULL
			JOIN exams e ON e.id = x.exam_id AND e.purpose IN ('seleksi', 'latihan') AND e.archived_at IS NULL
			WHERE p.school_id = $1 AND p.applicant_id = $2 AND x.ends_at > $3
			ORDER BY x.starts_at`, schoolID, applicantID, now.Add(-72*time.Hour))
		if err != nil {
			return fmt.Errorf("exam: card sessions: %w", err)
		}
		var windows []CardWindow
		var issuer string
		for rows.Next() {
			var t ApplicantTestCard
			var owner string
			if err := rows.Scan(&t.ExamName, &t.SessionID, &t.Start, &t.End, &t.Remote, &t.Practice, &t.Submitted, &owner); err != nil {
				rows.Close()
				return fmt.Errorf("exam: card sessions: %w", err)
			}
			t.Ended = !now.Before(t.End)
			if !t.Ended {
				windows = append(windows, CardWindow{SessionID: t.SessionID, Start: t.Start, End: t.End})
				if issuer == "" || !t.Practice {
					issuer = owner
				}
			}
			card.Tests = append(card.Tests, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return fmt.Errorf("exam: card sessions: %w", err)
		}
		if len(windows) == 0 {
			return nil
		}
		card.CodeFrom = windows[0].Start.Add(-CardLead)
		for _, w := range windows {
			if w.Start.Add(-CardLead).Before(card.CodeFrom) {
				card.CodeFrom = w.Start.Add(-CardLead)
			}
		}
		if now.Before(card.CodeFrom) {
			return nil
		}
		if card.Code, err = is.deriveCard(ctx, tx, schoolID, applicantID, windows); err != nil || card.Code != "" {
			return err
		}
		card.Code, err = is.IssueCard(ctx, tx, schoolID, issuer, Owner{ApplicantID: applicantID}, windows)
		return err
	})
	return card, err
}

// deriveCard menurunkan kode kartu pendaftar yang masih berlaku untuk
// semua windows (satu nonce yang sama, hash cocok). Kosong bila tidak ada.
func (is *Issuer) deriveCard(ctx context.Context, tx pgx.Tx, schoolID, applicantID string, windows []CardWindow) (string, error) {
	ids := make([]string, len(windows))
	for i, w := range windows {
		ids[i] = w.SessionID
	}
	rows, err := tx.Query(ctx, `SELECT exam_session_id::text, coalesce(code_nonce::text, ''), secret_hash FROM student_credentials
		WHERE school_id = $1 AND applicant_id = $2 AND kind = 'kode_ujian' AND revoked_at IS NULL AND exam_session_id = ANY($3::uuid[])`,
		schoolID, applicantID, ids)
	if err != nil {
		return "", fmt.Errorf("exam: card credentials: %w", err)
	}
	type cred struct{ nonce, hash string }
	bySession := map[string]cred{}
	for rows.Next() {
		var sid string
		var c cred
		if err := rows.Scan(&sid, &c.nonce, &c.hash); err != nil {
			rows.Close()
			return "", fmt.Errorf("exam: card credentials: %w", err)
		}
		bySession[sid] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("exam: card credentials: %w", err)
	}
	first, ok := bySession[ids[0]]
	if !ok || first.nonce == "" {
		return "", nil
	}
	for _, id := range ids {
		if c, ok := bySession[id]; !ok || c.nonce != first.nonce {
			return "", nil
		}
	}
	for i := range is.codes.keys {
		code := is.codes.cardCode(i, first.nonce)
		if ok, err := auth.VerifyPassword(code, first.hash); err != nil {
			return "", err
		} else if ok {
			return code, nil
		}
	}
	return "", nil
}
