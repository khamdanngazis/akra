package payments

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/storage"
	"akra/internal/school"
)

// MaxProofSize adalah ukuran maksimal bukti transfer (2 MB), sama dengan
// constraint payment_proofs_size_check.
const MaxProofSize = 2 << 20

// Aksi audit bukti transfer.
const (
	ActionProofUpload = "payment.proof_upload"
	ActionProofView   = "payment.proof_view"
)

// ProofURLTTL adalah masa berlaku URL bertanda tangan untuk membuka bukti.
const ProofURLTTL = 5 * time.Minute

var (
	// ErrPaymentNotFound dikembalikan jika pembayaran tidak ada atau milik
	// sekolah lain.
	ErrPaymentNotFound = errors.New("payments: payment not found")
	// ErrProofNotTransfer dikembalikan jika bukti dilampirkan ke pembayaran
	// tunai.
	ErrProofNotTransfer = errors.New("payments: proof is only for transfer payments")
	// ErrProofEmpty dikembalikan untuk file kosong.
	ErrProofEmpty = errors.New("payments: proof file is empty")
	// ErrProofTooLarge dikembalikan untuk file lebih dari MaxProofSize.
	ErrProofTooLarge = errors.New("payments: proof file too large")
	// ErrProofType dikembalikan jika isi file bukan JPEG, PNG, atau PDF.
	ErrProofType = errors.New("payments: proof must be JPEG, PNG, or PDF")
	// ErrProofNotFound dikembalikan jika bukti tidak ada atau milik sekolah lain.
	ErrProofNotFound = errors.New("payments: proof not found")
	// ErrProofDuplicate dikembalikan jika file yang sama sudah dipakai
	// sebagai bukti pembayaran lain di sekolah ini.
	ErrProofDuplicate = errors.New("payments: proof already used")
)

// proofTypes adalah tipe file yang diizinkan (dideteksi dari isi) beserta
// ekstensinya di storage.
var proofTypes = map[string]string{"image/jpeg": "jpg", "image/png": "png", "application/pdf": "pdf"}

// Proof adalah bukti transfer yang dilampirkan ke pembayaran.
type Proof struct {
	ID          string
	PaymentID   string
	StorageKey  string
	SHA256      []byte
	SizeBytes   int
	ContentType string
	UploadedBy  string
	UploadedAt  time.Time
}

// ProofService mengelola bukti transfer.
type ProofService struct {
	db    database.DB
	store storage.Store
}

// NewProofService mengembalikan ProofService.
func NewProofService(db database.DB, store storage.Store) *ProofService {
	return &ProofService{db: db, store: store}
}

// Upload membaca file bukti dari r (maksimal MaxProofSize), memeriksa
// tipenya dari isi file (bukan ekstensi atau header klien), menolak file
// yang sudah dipakai (SHA-256 sama di sekolah ini), menyimpannya ke storage
// dengan kunci schools/{school_id}/proofs/{uuid}.{ext}, lalu mencatat baris
// payment_proofs dan audit dalam satu transaksi. Jika pencatatan gagal,
// objek di storage dihapus lagi.
func (s *ProofService) Upload(ctx context.Context, actor school.Actor, paymentID string, r io.Reader) (Proof, error) {
	method, err := paymentMethod(ctx, s.db, actor.SchoolID, paymentID)
	if err != nil {
		return Proof{}, err
	}
	if method != MethodTransfer {
		return Proof{}, ErrProofNotTransfer
	}

	h := sha256.New()
	data, err := io.ReadAll(io.TeeReader(io.LimitReader(r, MaxProofSize+1), h))
	if err != nil {
		return Proof{}, fmt.Errorf("payments: read proof: %w", err)
	}
	switch {
	case len(data) == 0:
		return Proof{}, ErrProofEmpty
	case len(data) > MaxProofSize:
		return Proof{}, ErrProofTooLarge
	}
	contentType := http.DetectContentType(data)
	ext, ok := proofTypes[contentType]
	if !ok {
		return Proof{}, ErrProofType
	}
	sum := h.Sum(nil)
	if used, err := proofHashUsed(ctx, s.db, actor.SchoolID, sum); err != nil {
		return Proof{}, err
	} else if used {
		return Proof{}, ErrProofDuplicate
	}

	id, err := newUUID()
	if err != nil {
		return Proof{}, err
	}
	p := Proof{
		ID: id, PaymentID: paymentID, SHA256: sum, SizeBytes: len(data), ContentType: contentType, UploadedBy: actor.UserID,
		StorageKey: "schools/" + actor.SchoolID + "/proofs/" + id + "." + ext,
	}
	if err := s.store.Put(ctx, p.StorageKey, bytes.NewReader(data), int64(len(data)), contentType); err != nil {
		return Proof{}, err
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := insertProof(ctx, tx, actor.SchoolID, &p); err != nil {
			return err
		}
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionProofUpload, EntityType: "payment_proof", EntityID: p.ID,
			After: map[string]any{"payment_id": p.PaymentID, "content_type": p.ContentType, "size_bytes": p.SizeBytes, "sha256": fmt.Sprintf("%x", p.SHA256)},
		})
	})
	if err != nil {
		// Objek tanpa baris database tidak pernah bisa diakses; hapus agar
		// tidak menumpuk. Gagal hapus tidak menutupi error utama.
		_ = s.store.Delete(context.WithoutCancel(ctx), p.StorageKey)
		return Proof{}, err
	}
	return p, nil
}

// OpenURL mencatat audit akses (payment.proof_view) lalu mengembalikan URL
// bertanda tangan yang berlaku ProofURLTTL. Setiap pemanggilan dicatat; URL
// tidak pernah disimpan. Gambar dibuka inline, PDF diunduh (attachment).
func (s *ProofService) OpenURL(ctx context.Context, actor school.Actor, proofID string) (string, error) {
	if !database.IsUUID(proofID) {
		return "", ErrProofNotFound
	}
	var p Proof
	err := s.db.QueryRow(ctx, `
		SELECT pp.id, pp.payment_id, pp.storage_key, pp.content_type
		FROM payment_proofs pp JOIN payments p ON p.school_id = pp.school_id AND p.id = pp.payment_id
		WHERE pp.school_id = $1 AND pp.id = $2`, actor.SchoolID, proofID).Scan(&p.ID, &p.PaymentID, &p.StorageKey, &p.ContentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrProofNotFound
	}
	if err != nil {
		return "", mapError("get proof", err)
	}
	// Audit dulu: jika gagal dicatat, URL tidak diberikan.
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionProofView, EntityType: "payment_proof", EntityID: p.ID,
			After: map[string]any{"payment_id": p.PaymentID},
		})
	})
	if err != nil {
		return "", err
	}
	disposition := "inline"
	if p.ContentType == "application/pdf" {
		disposition = `attachment; filename="bukti-transfer-` + p.ID[:8] + `.pdf"`
	}
	return s.store.Presign(ctx, p.StorageKey, ProofURLTTL, storage.PresignOptions{ContentDisposition: disposition, ContentType: p.ContentType})
}

func paymentMethod(ctx context.Context, q database.DBTX, schoolID, paymentID string) (Method, error) {
	if !database.IsUUID(paymentID) {
		return "", ErrPaymentNotFound
	}
	var m Method
	err := q.QueryRow(ctx, `SELECT method FROM payments WHERE school_id = $1 AND id = $2`, schoolID, paymentID).Scan(&m)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPaymentNotFound
	}
	if err != nil {
		return "", mapError("get payment", err)
	}
	return m, nil
}

func proofHashUsed(ctx context.Context, q database.DBTX, schoolID string, sum []byte) (bool, error) {
	var used bool
	if err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM payment_proofs WHERE school_id = $1 AND sha256 = $2)`, schoolID, sum).Scan(&used); err != nil {
		return false, mapError("check proof hash", err)
	}
	return used, nil
}

func insertProof(ctx context.Context, tx pgx.Tx, schoolID string, p *Proof) error {
	err := tx.QueryRow(ctx, `
		INSERT INTO payment_proofs (id, school_id, payment_id, storage_key, sha256, size_bytes, content_type, uploaded_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING uploaded_at`,
		p.ID, schoolID, p.PaymentID, p.StorageKey, p.SHA256, p.SizeBytes, p.ContentType, p.UploadedBy).Scan(&p.UploadedAt)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "payment_proofs_school_sha256_key" {
		// Unggahan bersamaan dengan file yang sama: yang kedua kalah di sini.
		return ErrProofDuplicate
	}
	if err != nil {
		return mapError("insert proof", err)
	}
	return nil
}

// ListProofsByInvoice mengembalikan bukti semua pembayaran satu tagihan,
// dikelompokkan per id pembayaran.
func ListProofsByInvoice(ctx context.Context, q database.DBTX, schoolID, invoiceID string) (map[string][]Proof, error) {
	if !database.IsUUID(invoiceID) {
		return nil, nil
	}
	rows, err := q.Query(ctx, `
		SELECT pp.id, pp.payment_id, pp.storage_key, pp.sha256, pp.size_bytes, pp.content_type, pp.uploaded_by, pp.uploaded_at
		FROM payment_proofs pp JOIN payments p ON p.school_id = pp.school_id AND p.id = pp.payment_id
		WHERE pp.school_id = $1 AND p.invoice_id = $2
		ORDER BY pp.uploaded_at, pp.id`, schoolID, invoiceID)
	if err != nil {
		return nil, mapError("list proofs", err)
	}
	list, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Proof, error) {
		var p Proof
		err := row.Scan(&p.ID, &p.PaymentID, &p.StorageKey, &p.SHA256, &p.SizeBytes, &p.ContentType, &p.UploadedBy, &p.UploadedAt)
		return p, err
	})
	if err != nil {
		return nil, mapError("list proofs", err)
	}
	out := map[string][]Proof{}
	for _, p := range list {
		out[p.PaymentID] = append(out[p.PaymentID], p)
	}
	return out, nil
}

// InvoiceIDOfPayment mengembalikan tagihan milik pembayaran.
func (s *ProofService) InvoiceIDOfPayment(ctx context.Context, schoolID, paymentID string) (string, error) {
	if !database.IsUUID(paymentID) {
		return "", ErrPaymentNotFound
	}
	var id string
	err := s.db.QueryRow(ctx, `SELECT invoice_id FROM payments WHERE school_id = $1 AND id = $2`, schoolID, paymentID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrPaymentNotFound
	}
	if err != nil {
		return "", mapError("get payment invoice", err)
	}
	return id, nil
}

// newUUID membuat UUID v4 acak.
func newUUID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("payments: generate id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
