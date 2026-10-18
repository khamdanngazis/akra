package importer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/school"
)

// Status batch impor.
const (
	StatusUploaded  = "diunggah"
	StatusValidated = "divalidasi"
	StatusRunning   = "berjalan"
	StatusDone      = "selesai"
	StatusFailed    = "gagal"
)

// ErrBatchNotFound: batch tidak ada atau milik sekolah lain.
var ErrBatchNotFound = errors.New("importer: batch not found")

// previewRows adalah jumlah baris yang ditampilkan di halaman batch.
const previewRows = 5

// Batch adalah satu unggahan file impor.
type Batch struct {
	ID          string
	FileName    string
	Status      string
	TotalRows   int
	SkippedRows int
	Skipped     []Skip
	Preview     []Row
	UploadedBy  string // nama pengunggah
	CreatedAt   time.Time
}

// Service menyimpan dan membaca batch impor.
type Service struct {
	db database.DBTX
}

// NewService mengembalikan Service.
func NewService(db database.DBTX) *Service {
	return &Service{db: db}
}

// CreateBatch menyimpan hasil parsing sebagai batch berstatus diunggah.
// Belum ada data siswa yang ditulis.
func (s *Service) CreateBatch(ctx context.Context, actor school.Actor, fileName string, rows Rows) (string, error) {
	parsed, err := json.Marshal(rows.Data)
	if err != nil {
		return "", fmt.Errorf("importer: encode rows: %w", err)
	}
	skipped := rows.Skipped
	if skipped == nil {
		skipped = []Skip{}
	}
	skippedJSON, err := json.Marshal(skipped)
	if err != nil {
		return "", fmt.Errorf("importer: encode skipped: %w", err)
	}
	var id string
	err = s.db.QueryRow(ctx, `
		INSERT INTO import_batches (school_id, uploaded_by, file_name, status, total_rows, skipped_rows, parsed_rows, skipped)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
		actor.SchoolID, actor.UserID, cleanFileName(fileName), StatusUploaded, len(rows.Data), len(rows.Skipped), parsed, skippedJSON).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("importer: insert batch: %w", err)
	}
	return id, nil
}

// GetBatch mengembalikan batch milik sekolah, dengan pratinjau beberapa
// baris pertama.
func (s *Service) GetBatch(ctx context.Context, schoolID, id string) (Batch, error) {
	if !database.IsUUID(id) {
		return Batch{}, ErrBatchNotFound
	}
	var (
		b                Batch
		preview, skipped []byte
	)
	err := s.db.QueryRow(ctx, `
		SELECT b.id, b.file_name, b.status, b.total_rows, b.skipped_rows, b.skipped,
		       coalesce(jsonb_path_query_array(b.parsed_rows, '$[0 to `+fmt.Sprint(previewRows-1)+`]'), '[]'),
		       u.full_name, b.created_at
		FROM import_batches b
		JOIN users u ON u.school_id = b.school_id AND u.id = b.uploaded_by
		WHERE b.school_id = $1 AND b.id = $2`, schoolID, id).
		Scan(&b.ID, &b.FileName, &b.Status, &b.TotalRows, &b.SkippedRows, &skipped, &preview, &b.UploadedBy, &b.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Batch{}, ErrBatchNotFound
	}
	if err != nil {
		return Batch{}, fmt.Errorf("importer: get batch: %w", err)
	}
	if err := json.Unmarshal(skipped, &b.Skipped); err != nil {
		return Batch{}, fmt.Errorf("importer: decode skipped: %w", err)
	}
	if err := json.Unmarshal(preview, &b.Preview); err != nil {
		return Batch{}, fmt.Errorf("importer: decode preview: %w", err)
	}
	return b, nil
}

// ParsedRows mengembalikan seluruh baris hasil parsing batch (untuk validasi
// T54).
func (s *Service) ParsedRows(ctx context.Context, schoolID, id string) ([]Row, error) {
	if !database.IsUUID(id) {
		return nil, ErrBatchNotFound
	}
	var raw []byte
	err := s.db.QueryRow(ctx, `SELECT coalesce(parsed_rows, '[]') FROM import_batches WHERE school_id = $1 AND id = $2`, schoolID, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrBatchNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("importer: get parsed rows: %w", err)
	}
	var rows []Row
	if err := json.Unmarshal(raw, &rows); err != nil {
		return nil, fmt.Errorf("importer: decode parsed rows: %w", err)
	}
	return rows, nil
}

// cleanFileName menyimpan nama file saja (tanpa folder dari browser lama),
// maksimal 255 karakter.
func cleanFileName(name string) string {
	name = strings.ToValidUTF8(strings.TrimSpace(path.Base(strings.ReplaceAll(name, `\`, "/"))), "")
	if name == "" || name == "." || name == "/" {
		name = "impor.xlsx"
	}
	for utf8.RuneCountInString(name) > 255 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return name
}
