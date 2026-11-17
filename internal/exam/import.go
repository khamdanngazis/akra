package exam

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"akra/internal/exam/mathrender"
	"akra/internal/exam/richtext"
	"akra/internal/platform/database"
	"akra/internal/platform/storage"
)

// Sumber impor.
const (
	SourceWord   = "word"
	SourceMoodle = "moodle"
	SourceGIFT   = "gift"
	SourceExcel  = "excel"
)

// Status impor dan butir.
const (
	importProcessing = "memproses"
	importReady      = "siap"
	importFailed     = "gagal"
	importDone       = "selesai"

	ItemReady = "siap"
	ItemCheck = "cek"
	ItemSkip  = "lewati"
)

// Parser membaca file impor menjadi butir. Dipasang per sumber.
type Parser func(data []byte, save ImageSaver) ([]ParsedItem, error)

// parsers berisi pembaca per sumber; Moodle, GIFT, dan Excel ditambahkan
// di berkasnya masing-masing (T130, T131).
var parsers = map[string]Parser{SourceWord: ParseDocx}

// Import adalah satu proses impor.
type Import struct {
	ID         string
	SchoolID   string
	BankID     string
	Source     string
	FileName   string
	FileKey    string
	Status     string
	Progress   int
	Total      int
	Error      string
	Result     ImportResult
	CreatedBy  string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// ImportResult adalah laporan akhir impor.
type ImportResult struct {
	Ready   int `json:"ready"`
	Draft   int `json:"draft"`
	Skipped int `json:"skipped"`
	Stimuli int `json:"stimuli"`
}

// ImportItem adalah satu butir hasil baca yang bisa diperbaiki.
type ImportItem struct {
	Seq         int
	Kind        string
	Status      string
	Problems    []string
	Form        QuestionForm  // kind soal
	Stim        StimulusDraft // kind stimulus
	StimulusSeq int           // -1 bila tanpa stimulus
	Origin      string
	Number      string
}

// ErrImportFile: file tidak bisa dibaca sebagai sumber yang dipilih.
var ErrImportFile = errors.New("exam: import file unreadable")

// ImportJobInserter memasukkan job impor (River). nil = diproses langsung.
type ImportJobInserter interface {
	Insert(ctx context.Context, args river.JobArgs, opts *river.InsertOpts) (*rivertype.JobInsertResult, error)
}

// ImportArgs adalah argumen job pembacaan impor.
type ImportArgs struct {
	SchoolID string `json:"school_id"`
	ImportID string `json:"import_id"`
}

// Kind mengembalikan nama jenis job.
func (ImportArgs) Kind() string { return "exam_import" }

// CreateImport menyimpan file ke storage dan mencatat impor berstatus
// memproses. Pembacaan dijalankan ProcessImport (langsung atau lewat job).
func CreateImport(ctx context.Context, db database.DB, store storage.Store, sa StaffActor, bankID, source, fileName string, data []byte, now time.Time) (Import, error) {
	if _, ok := parsers[source]; !ok {
		return Import{}, &FieldError{Fields: map[string]string{"file": "Jenis file tidak dikenal."}}
	}
	if len(data) == 0 || len(data) > MaxImportBytes {
		return Import{}, &FieldError{Fields: map[string]string{"file": "File kosong atau lebih dari 20 MB."}}
	}
	b, err := GetBank(ctx, db, sa.SchoolID, bankID)
	if err != nil {
		return Import{}, err
	}
	if b.ArchivedAt != nil || sa.Can(ActBank, b.resource(), now) < Write {
		return Import{}, ErrNotEditable
	}
	fileName = strings.TrimSpace(fileName)
	if !utf8.ValidString(fileName) || len(fileName) > 200 {
		fileName = "impor"
	}
	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return Import{}, err
	}
	key := fmt.Sprintf("schools/%s/exam-imports/%s", sa.SchoolID, hex.EncodeToString(rnd[:]))
	if err := store.Put(ctx, key, bytes.NewReader(data), int64(len(data)), "application/octet-stream"); err != nil {
		return Import{}, fmt.Errorf("exam: store import file: %w", err)
	}
	imp := Import{SchoolID: sa.SchoolID, BankID: bankID, Source: source, FileName: fileName, FileKey: key, Status: importProcessing, CreatedBy: sa.UserID}
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO exam_imports (school_id, bank_id, source, file_name, file_key, created_by)
			VALUES ($1, $2, $3, $4, $5, $6) RETURNING id, created_at`, sa.SchoolID, bankID, source, fileName, key, sa.UserID).Scan(&imp.ID, &imp.CreatedAt); err != nil {
			return fmt.Errorf("exam: create import: %w", err)
		}
		return recordAudit(ctx, tx, sa, "exam_import.create", "exam_import", imp.ID, nil, map[string]any{"bank_id": bankID, "source": source, "file": fileName})
	})
	if err != nil {
		_ = store.Delete(ctx, key)
		return Import{}, err
	}
	return imp, nil
}

// GetImport mengembalikan impor milik sekolah.
func GetImport(ctx context.Context, q database.DBTX, schoolID, id string) (Import, error) {
	if !database.IsUUID(id) {
		return Import{}, ErrNotFound
	}
	var imp Import
	var result []byte
	err := q.QueryRow(ctx, `SELECT id, school_id, bank_id, source, file_name, coalesce(file_key, ''), status, progress, total, error, result,
		created_by, created_at, finished_at FROM exam_imports WHERE school_id = $1 AND id = $2`, schoolID, id).
		Scan(&imp.ID, &imp.SchoolID, &imp.BankID, &imp.Source, &imp.FileName, &imp.FileKey, &imp.Status, &imp.Progress, &imp.Total,
			&imp.Error, &result, &imp.CreatedBy, &imp.CreatedAt, &imp.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Import{}, ErrNotFound
	}
	if err != nil {
		return Import{}, fmt.Errorf("exam: get import: %w", err)
	}
	_ = json.Unmarshal(result, &imp.Result)
	return imp, nil
}

// importMessage menerjemahkan error baca file untuk guru.
func importMessage(err error) string {
	switch {
	case errors.Is(err, ErrDocxMacro):
		return "Dokumen berisi makro. Simpan ulang di Word sebagai \"Dokumen Word (.docx)\" lalu unggah lagi."
	case errors.Is(err, ErrNotDocx):
		return "File bukan dokumen Word .docx yang bisa dibaca. Untuk .doc lama, buka di Word lalu simpan sebagai .docx."
	case errors.Is(err, ErrTooManyItems):
		return fmt.Sprintf("File berisi lebih dari %d soal. Pecah menjadi beberapa file.", MaxImportQuestions)
	case errors.Is(err, ErrImportFile):
		return "File tidak bisa dibaca. Periksa formatnya sesuai petunjuk."
	}
	var fe *FieldError
	if errors.As(err, &fe) {
		for _, m := range fe.Fields {
			return m
		}
	}
	return "File tidak bisa dibaca."
}

// ProcessImport membaca file impor, memeriksa setiap butir, dan menyimpan
// hasilnya untuk pratinjau.
func ProcessImport(ctx context.Context, db database.DB, store storage.Store, math *mathrender.Renderer, schoolID, importID string) error {
	imp, err := GetImport(ctx, db, schoolID, importID)
	if err != nil {
		return err
	}
	if imp.Status != importProcessing {
		return nil // sudah diproses (job diulang)
	}
	fail := func(cause error) error {
		_, err := db.Exec(ctx, `UPDATE exam_imports SET status = 'gagal', error = $3, finished_at = now() WHERE school_id = $1 AND id = $2`,
			schoolID, importID, importMessage(cause))
		return err
	}
	rc, err := store.Get(ctx, imp.FileKey)
	if err != nil {
		return fmt.Errorf("exam: open import file: %w", err)
	}
	data, err := io.ReadAll(io.LimitReader(rc, MaxImportBytes+1))
	rc.Close()
	if err != nil {
		return fmt.Errorf("exam: read import file: %w", err)
	}
	sa := StaffActor{Actor: Actor{UserID: imp.CreatedBy}, SchoolID: schoolID}
	save := func(img []byte, name string) (string, error) {
		a, err := SaveAsset(ctx, db, store, sa, imp.BankID, img)
		return a.ID, err
	}
	items, err := parsers[imp.Source](data, save)
	if err != nil {
		return fail(err)
	}
	b, err := GetBank(ctx, db, schoolID, imp.BankID)
	if err != nil {
		return err
	}
	var objectives []Objective
	if b.SubjectID != "" {
		if objectives, err = ListObjectives(ctx, db, schoolID, b.SubjectID, 0); err != nil {
			return err
		}
	}
	if _, err := db.Exec(ctx, `UPDATE exam_imports SET total = $3 WHERE school_id = $1 AND id = $2`, schoolID, importID, len(items)); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM exam_import_items WHERE import_id = $1`, importID); err != nil {
			return err
		}
		for i, it := range items {
			if it.Kind == "soal" && it.TPCode != "" {
				if id := matchObjective(objectives, it.TPCode); id != "" {
					it.Form.ObjectiveID = id
				} else {
					it.Problems = append(it.Problems, "TP "+it.TPCode+" belum ada di daftar TP mapel ini")
				}
			}
			ii := checkItem(it, math)
			ii.Seq = i
			if err := insertItem(ctx, tx, schoolID, importID, ii); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `UPDATE exam_imports SET status = 'siap', progress = total, finished_at = now() WHERE school_id = $1 AND id = $2`, schoolID, importID)
		return err
	})
}

// matchObjective mencari TP bank dengan kode "3.2", "TP 3.2", dsb.
func matchObjective(objs []Objective, code string) string {
	c := strings.ToLower(strings.TrimSpace(code))
	for _, o := range objs {
		oc := strings.ToLower(o.Code)
		if oc == c || oc == "tp "+c || strings.TrimPrefix(oc, "tp ") == strings.TrimPrefix(c, "tp ") {
			return o.ID
		}
	}
	return ""
}

// checkItem menentukan status butir: lewati, perlu dicek, atau siap.
func checkItem(it ParsedItem, math *mathrender.Renderer) ImportItem {
	ii := ImportItem{Kind: it.Kind, Problems: it.Problems, Form: it.Form, Stim: it.Stim, StimulusSeq: it.StimIdx, Origin: it.Origin, Number: it.Number}
	switch {
	case it.Skip:
		ii.Status = ItemSkip
	case it.Kind == "stimulus":
		doc, err := richtext.Process(it.Stim.Body, math)
		if err != nil || len(doc.MathErrors) > 0 {
			ii.Problems = append(ii.Problems, "rumus di stimulus tidak valid")
		}
		ii.Status = ItemReady
		if len(ii.Problems) > 0 {
			ii.Status = ItemCheck
		}
	default:
		f := it.Form
		f.Status = StatusReady
		_, _, err := BuildQuestion(f, math)
		var fe *FieldError
		if errors.As(err, &fe) {
			for _, m := range fe.Fields {
				ii.Problems = append(ii.Problems, m)
			}
		}
		ii.Status = ItemReady
		if len(ii.Problems) > 0 {
			ii.Status = ItemCheck
		}
	}
	return ii
}

func insertItem(ctx context.Context, tx pgx.Tx, schoolID, importID string, ii ImportItem) error {
	var data []byte
	var err error
	if ii.Kind == "stimulus" {
		data, err = json.Marshal(ii.Stim)
	} else {
		data, err = json.Marshal(ii.Form.Values())
	}
	if err != nil {
		return err
	}
	var stim any
	if ii.StimulusSeq >= 0 && ii.Kind == "soal" {
		stim = ii.StimulusSeq
	}
	if ii.Problems == nil {
		ii.Problems = []string{}
	}
	_, err = tx.Exec(ctx, `INSERT INTO exam_import_items (import_id, school_id, seq, kind, status, problems, data, stimulus_seq, origin, number)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		importID, schoolID, ii.Seq, ii.Kind, ii.Status, ii.Problems, data, stim, Snippet(ii.Origin, 500), ii.Number)
	if err != nil {
		return fmt.Errorf("exam: insert import item: %w", err)
	}
	return nil
}

// ListImportItems mengembalikan semua butir impor sesuai urutan.
func ListImportItems(ctx context.Context, q database.DBTX, schoolID, importID string) ([]ImportItem, error) {
	rows, err := q.Query(ctx, `SELECT seq, kind, status, problems, data, coalesce(stimulus_seq, -1), origin, number
		FROM exam_import_items WHERE school_id = $1 AND import_id = $2 ORDER BY seq`, schoolID, importID)
	if err != nil {
		return nil, fmt.Errorf("exam: list import items: %w", err)
	}
	defer rows.Close()
	var out []ImportItem
	for rows.Next() {
		var ii ImportItem
		var data []byte
		if err := rows.Scan(&ii.Seq, &ii.Kind, &ii.Status, &ii.Problems, &data, &ii.StimulusSeq, &ii.Origin, &ii.Number); err != nil {
			return nil, fmt.Errorf("exam: list import items: %w", err)
		}
		if ii.Kind == "stimulus" {
			if err := json.Unmarshal(data, &ii.Stim); err != nil {
				return nil, err
			}
		} else {
			var v map[string][]string
			if err := json.Unmarshal(data, &v); err != nil {
				return nil, err
			}
			ii.Form = mergeForm(emptyForm(""), ParseQuestionForm(v))
		}
		out = append(out, ii)
	}
	return out, rows.Err()
}

// SaveImportItem menyimpan perbaikan satu butir di pratinjau lalu
// memeriksanya ulang.
func SaveImportItem(ctx context.Context, db database.DB, math *mathrender.Renderer, imp Import, seq int, f QuestionForm, st StimulusDraft) (ImportItem, error) {
	if imp.Status != importReady {
		return ImportItem{}, ErrNotEditable
	}
	items, err := ListImportItems(ctx, db, imp.SchoolID, imp.ID)
	if err != nil {
		return ImportItem{}, err
	}
	if seq < 0 || seq >= len(items) || items[seq].Seq != seq {
		return ImportItem{}, ErrNotFound
	}
	cur := items[seq]
	p := ParsedItem{Kind: cur.Kind, Number: cur.Number, Form: f, Stim: st, StimIdx: cur.StimulusSeq, Origin: cur.Origin}
	if cur.Kind == "soal" {
		p.Form.StimulusID = ""
	}
	ii := checkItem(p, math)
	ii.Seq = seq
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM exam_import_items WHERE import_id = $1 AND seq = $2`, imp.ID, seq); err != nil {
			return err
		}
		return insertItem(ctx, tx, imp.SchoolID, imp.ID, ii)
	})
	return ii, err
}

// CommitOptions mengatur penyimpanan hasil impor.
type CommitOptions struct {
	// IncludeCheck: butir "perlu dicek" ikut disimpan sebagai draf.
	IncludeCheck bool
	// Exclude berisi seq butir yang tidak disimpan.
	Exclude map[int]bool
	// Stimulus mengganti stimulus soal: seq soal → seq stimulus (-1 tanpa).
	Stimulus map[int]int
	// Bawaan untuk soal yang belum punya TP, kesulitan, atau tag.
	ObjectiveID string
	Difficulty  string
	Tags        string
}

// CommitImport menyimpan semua butir terpilih dalam satu transaksi: gagal
// di tengah tidak menyimpan sebagian.
func CommitImport(ctx context.Context, db database.DB, math *mathrender.Renderer, sa StaffActor, imp Import, opt CommitOptions, now time.Time) (ImportResult, error) {
	var res ImportResult
	if imp.Status != importReady {
		return res, ErrNotEditable
	}
	items, err := ListImportItems(ctx, db, sa.SchoolID, imp.ID)
	if err != nil {
		return res, err
	}
	stimOf := func(it ImportItem) int {
		if s, ok := opt.Stimulus[it.Seq]; ok {
			return s
		}
		return it.StimulusSeq
	}
	include := func(it ImportItem) bool {
		return !opt.Exclude[it.Seq] && (it.Status == ItemReady || (it.Status == ItemCheck && opt.IncludeCheck))
	}
	usedStim := map[int]bool{}
	for _, it := range items {
		if it.Kind == "soal" && include(it) && stimOf(it) >= 0 {
			usedStim[stimOf(it)] = true
		}
	}
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		stimIDs := map[int]string{}
		for _, it := range items {
			if it.Kind != "stimulus" || !usedStim[it.Seq] {
				continue
			}
			doc, err := richtext.Process(it.Stim.Body, math)
			if err != nil {
				return err
			}
			if err := checkAssets(ctx, tx, sa.SchoolID, doc.Assets); err != nil {
				return err
			}
			s, err := createStimulusTx(ctx, tx, sa, imp.BankID, Stimulus{Title: it.Stim.Title, BodySrc: doc.Src, BodyHTML: doc.HTML, Numbered: it.Stim.Numbered}, now)
			if err != nil {
				return fmt.Errorf("stimulus %d: %w", it.Seq, err)
			}
			stimIDs[it.Seq] = s.ID
			res.Stimuli++
		}
		for _, it := range items {
			if it.Kind != "soal" {
				continue
			}
			if !include(it) {
				res.Skipped++
				continue
			}
			f := it.Form
			if f.ObjectiveID == "" {
				f.ObjectiveID = opt.ObjectiveID
			}
			if f.Difficulty == "" {
				f.Difficulty = opt.Difficulty
			}
			if strings.TrimSpace(f.Tags) == "" {
				f.Tags = opt.Tags
			} else if opt.Tags != "" {
				f.Tags += "," + opt.Tags
			}
			f.StimulusID = stimIDs[stimOf(it)]
			f.Status = StatusReady
			if it.Status == ItemCheck {
				f.Status = StatusDraft
			}
			q, assets, err := BuildQuestion(f, math)
			if err != nil && f.Status == StatusReady {
				f.Status = StatusDraft
				q, assets, err = BuildQuestion(f, math)
			}
			if err != nil {
				return fmt.Errorf("soal %s: %w", it.Number, err)
			}
			if err := checkAssets(ctx, tx, sa.SchoolID, assets); err != nil {
				return err
			}
			if _, err := createQuestionTx(ctx, tx, sa, imp.BankID, q, now); err != nil {
				return fmt.Errorf("soal %s: %w", it.Number, err)
			}
			if q.Status == StatusReady {
				res.Ready++
			} else {
				res.Draft++
			}
		}
		result, _ := json.Marshal(res)
		if _, err := tx.Exec(ctx, `UPDATE exam_imports SET status = 'selesai', result = $3, finished_at = now() WHERE school_id = $1 AND id = $2`,
			sa.SchoolID, imp.ID, result); err != nil {
			return err
		}
		return recordAudit(ctx, tx, sa, "exam_import.commit", "exam_import", imp.ID, nil, res)
	})
	return res, err
}

// ImportWorker menjalankan job pembacaan impor.
type ImportWorker struct {
	river.WorkerDefaults[ImportArgs]
	DB     database.DB
	Store  storage.Store
	Math   *mathrender.Renderer
	Logger *slog.Logger
}

// Work membaca satu file impor.
func (w *ImportWorker) Work(ctx context.Context, job *river.Job[ImportArgs]) error {
	err := ProcessImport(ctx, w.DB, w.Store, w.Math, job.Args.SchoolID, job.Args.ImportID)
	if err != nil && job.Attempt >= job.MaxAttempts {
		w.Logger.ErrorContext(ctx, "exam import failed", "import_id", job.Args.ImportID, "error", err)
		_, _ = w.DB.Exec(context.Background(), `UPDATE exam_imports SET status = 'gagal', error = 'Pemrosesan gagal. Coba unggah ulang.', finished_at = now()
			WHERE school_id = $1 AND id = $2 AND status = 'memproses'`, job.Args.SchoolID, job.Args.ImportID)
	}
	return err
}

// Timeout membatasi lama pembacaan satu file.
func (w *ImportWorker) Timeout(*river.Job[ImportArgs]) time.Duration { return 5 * time.Minute }

// Total mengembalikan jumlah soal yang masuk.
func (r ImportResult) Total() int { return r.Ready + r.Draft }
