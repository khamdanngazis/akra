package admission

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

	"akra/internal/audit"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/platform/storage"
	"akra/internal/school"
)

// MaxDocumentSize adalah ukuran maksimal satu dokumen (2 MB), sama dengan
// constraint applicant_documents_size_check.
const MaxDocumentSize = 2 << 20

// ActionDocumentView dicatat setiap staf membuka dokumen pendaftar.
const ActionDocumentView = "admission.document_view"

// docTypes adalah tipe file yang diizinkan (dideteksi dari isi).
var docTypes = map[string]bool{"image/jpeg": true, "image/png": true, "application/pdf": true}

// DocLabel mengembalikan nama dokumen untuk ditampilkan.
func DocLabel(key string) string {
	for _, d := range Docs {
		if d.Key == key {
			return d.Label
		}
	}
	if key == "lainnya" {
		return "Dokumen lain"
	}
	return key
}

func validDocType(key string) bool {
	return key == "lainnya" || DocLabel(key) != key
}

var (
	// ErrDocEmpty dikembalikan untuk file kosong.
	ErrDocEmpty = errors.New("admission: document is empty")
	// ErrDocTooLarge dikembalikan untuk file lebih dari MaxDocumentSize.
	ErrDocTooLarge = errors.New("admission: document too large")
	// ErrDocType dikembalikan jika isi file bukan JPEG, PNG, atau PDF.
	ErrDocType = errors.New("admission: document must be JPEG, PNG, or PDF")
)

// Document adalah satu dokumen pendaftar.
type Document struct {
	ID          string
	ApplicantID string
	DocType     string
	Label       string
	StorageKey  string
	ContentType string
	SizeBytes   int
	UploadedAt  time.Time
}

// DocumentService menyimpan dokumen pendaftar terenkripsi di storage.
type DocumentService struct {
	db    database.DB
	store storage.Store
	keys  *pii.Keyring
}

// NewDocumentService mengembalikan DocumentService.
func NewDocumentService(db database.DB, store storage.Store, keys *pii.Keyring) *DocumentService {
	return &DocumentService{db: db, store: store, keys: keys}
}

// docAAD mengikat ciphertext file ke baris dokumennya.
func docAAD(id string) string { return "applicant_documents:" + id }

// Upload membaca file (maksimal MaxDocumentSize), memeriksa tipenya dari
// isi, mengenkripsinya, lalu menyimpannya. Dokumen dengan jenis yang sama
// menggantikan yang lama. Hanya pendaftar yang masih bisa diubah (draf atau
// perlu perbaikan) yang bisa mengunggah.
func (s *DocumentService) Upload(ctx context.Context, schoolID, applicantID, docType string, r io.Reader) (Document, error) {
	if !validDocType(docType) {
		return Document{}, fmt.Errorf("admission: unknown document type %q", docType)
	}
	data, err := io.ReadAll(io.LimitReader(r, MaxDocumentSize+1))
	if err != nil {
		return Document{}, fmt.Errorf("admission: read document: %w", err)
	}
	switch {
	case len(data) == 0:
		return Document{}, ErrDocEmpty
	case len(data) > MaxDocumentSize:
		return Document{}, ErrDocTooLarge
	}
	contentType := http.DetectContentType(data)
	if !docTypes[contentType] {
		return Document{}, ErrDocType
	}
	sum := sha256.Sum256(data)
	id, err := newID()
	if err != nil {
		return Document{}, err
	}
	ct, err := s.keys.Encrypt(string(data), docAAD(id))
	if err != nil {
		return Document{}, err
	}
	d := Document{ID: id, ApplicantID: applicantID, DocType: docType, Label: DocLabel(docType), ContentType: contentType, SizeBytes: len(data),
		StorageKey: "schools/" + schoolID + "/applicants/" + applicantID + "/" + id + ".enc"}

	// Pastikan pendaftar ada dan bisa diubah sebelum menulis ke storage.
	if err := s.checkEditable(ctx, s.db, schoolID, applicantID, false); err != nil {
		return Document{}, err
	}
	if err := s.store.Put(ctx, d.StorageKey, bytes.NewReader(ct), int64(len(ct)), "application/octet-stream"); err != nil {
		return Document{}, err
	}
	var oldKey string
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if err := s.checkEditable(ctx, tx, schoolID, applicantID, true); err != nil {
			return err
		}
		err := tx.QueryRow(ctx, `SELECT storage_key FROM applicant_documents WHERE school_id = $1 AND applicant_id = $2 AND doc_type = $3`,
			schoolID, applicantID, docType).Scan(&oldKey)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if oldKey != "" {
			if _, err := tx.Exec(ctx, `DELETE FROM applicant_documents WHERE school_id = $1 AND applicant_id = $2 AND doc_type = $3`,
				schoolID, applicantID, docType); err != nil {
				return err
			}
		}
		return tx.QueryRow(ctx, `
			INSERT INTO applicant_documents (id, school_id, applicant_id, doc_type, storage_key, content_type, size_bytes, sha256)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING uploaded_at`,
			d.ID, schoolID, applicantID, docType, d.StorageKey, contentType, len(data), sum[:]).Scan(&d.UploadedAt)
	})
	if err != nil {
		// Objek tanpa baris database tidak pernah bisa diakses; hapus.
		_ = s.store.Delete(context.WithoutCancel(ctx), d.StorageKey)
		return Document{}, mapApplicantError(err)
	}
	if oldKey != "" {
		_ = s.store.Delete(context.WithoutCancel(ctx), oldKey)
	}
	return d, nil
}

func (s *DocumentService) checkEditable(ctx context.Context, q database.DBTX, schoolID, applicantID string, forUpdate bool) error {
	if !database.IsUUID(applicantID) {
		return ErrNotFound
	}
	sql := `SELECT status FROM applicants WHERE school_id = $1 AND id = $2`
	if forUpdate {
		sql += ` FOR UPDATE`
	}
	var status string
	if err := q.QueryRow(ctx, sql, schoolID, applicantID).Scan(&status); err != nil {
		return mapApplicantError(err)
	}
	if status != ApplicantDraft && status != ApplicantFix {
		return ErrNotEditable
	}
	return nil
}

// List mengembalikan dokumen pendaftar, urut seperti Docs.
func (s *DocumentService) List(ctx context.Context, schoolID, applicantID string) ([]Document, error) {
	if !database.IsUUID(applicantID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Query(ctx, `SELECT id, applicant_id, doc_type, storage_key, content_type, size_bytes, uploaded_at
		FROM applicant_documents WHERE school_id = $1 AND applicant_id = $2 ORDER BY uploaded_at`, schoolID, applicantID)
	if err != nil {
		return nil, mapApplicantError(err)
	}
	docs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Document, error) {
		var d Document
		err := r.Scan(&d.ID, &d.ApplicantID, &d.DocType, &d.StorageKey, &d.ContentType, &d.SizeBytes, &d.UploadedAt)
		d.Label = DocLabel(d.DocType)
		return d, err
	})
	if err != nil {
		return nil, mapApplicantError(err)
	}
	return docs, nil
}

// Open mengambil dan mendekripsi dokumen untuk staf, lalu mencatat audit
// akses. Dokumen sekolah lain mengembalikan ErrNotFound.
func (s *DocumentService) Open(ctx context.Context, actor school.Actor, applicantID, docID string) (Document, []byte, error) {
	if !database.IsUUID(applicantID) || !database.IsUUID(docID) {
		return Document{}, nil, ErrNotFound
	}
	var d Document
	err := s.db.QueryRow(ctx, `SELECT id, applicant_id, doc_type, storage_key, content_type, size_bytes, uploaded_at
		FROM applicant_documents WHERE school_id = $1 AND applicant_id = $2 AND id = $3`, actor.SchoolID, applicantID, docID).
		Scan(&d.ID, &d.ApplicantID, &d.DocType, &d.StorageKey, &d.ContentType, &d.SizeBytes, &d.UploadedAt)
	if err != nil {
		return Document{}, nil, mapApplicantError(err)
	}
	d.Label = DocLabel(d.DocType)
	body, err := s.store.Get(ctx, d.StorageKey)
	if err != nil {
		return Document{}, nil, fmt.Errorf("admission: open document: %w", err)
	}
	defer body.Close()
	ct, err := io.ReadAll(io.LimitReader(body, MaxDocumentSize+1024))
	if err != nil {
		return Document{}, nil, fmt.Errorf("admission: read document: %w", err)
	}
	plain, err := s.keys.Decrypt(ct, docAAD(d.ID))
	if err != nil {
		return Document{}, nil, fmt.Errorf("admission: decrypt document: %w", err)
	}
	err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, audit.Entry{
			SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionDocumentView, EntityType: "applicant", EntityID: applicantID,
			After: map[string]any{"document_id": d.ID, "doc_type": d.DocType},
		})
	})
	if err != nil {
		return Document{}, nil, err
	}
	return d, []byte(plain), nil
}

// MissingDocs mengembalikan dokumen wajib periode yang belum diunggah.
func MissingDocs(p Period, docs []Document) []string {
	have := map[string]bool{}
	for _, d := range docs {
		have[d.DocType] = true
	}
	var out []string
	for _, k := range p.RequiredDocs {
		if !have[k] {
			out = append(out, k)
		}
	}
	return out
}

func newID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("admission: generate id: %w", err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16]), nil
}
