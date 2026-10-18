package importer

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	studentPath      = "/impor/siswa"
	batchPath        = "/impor/batch"
	templateFilename = "template-impor-siswa.xlsx"
	// maxUpload adalah batas body unggahan: file plus field form lain.
	maxUpload = MaxFileSize + 64<<10
)

// Handler melayani halaman impor.
type Handler struct {
	svc      *Service
	sessions *scs.SessionManager
	render   page.Renderer
	logger   *slog.Logger
	today    func() time.Time
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, sessions: sessions, render: render, logger: logger, today: clock.Today}
}

// Routes mendaftarkan route impor.
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(auth.RequireRole(auth.RoleAdminTU))
		// Peran: admin_tu.
		r.Get(studentPath, h.index)
		// Peran: admin_tu.
		r.Get(studentPath+"/template.xlsx", h.template)
		// Peran: admin_tu.
		r.Post(studentPath, h.upload)
		// Peran: admin_tu.
		r.Get(batchPath+"/{id}", h.batch)
		// Peran: admin_tu.
		r.Post(batchPath+"/{id}/simpan", h.run)
		// Peran: admin_tu.
		r.Get(batchPath+"/{id}/kesalahan.xlsx", h.errorReport)
	})
}

type indexView struct {
	User     auth.Identity
	Columns  []columnRow
	Filename string
	MaxBytes int
	Flash    string
	Error    string
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderIndex(w, r, id, http.StatusOK, "")
}

func (h *Handler) renderIndex(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, errMsg string) {
	rows := make([]columnRow, len(StudentColumns))
	for i, c := range StudentColumns {
		rows[i] = columnRow{Letter: string(rune('A' + i)), Column: c}
	}
	v := indexView{User: id, Columns: rows, Filename: templateFilename, MaxBytes: MaxFileSize, Error: errMsg}
	if errMsg == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	h.render.Render(w, r, status, "importer/index", v)
}

// parseErrors adalah pesan untuk file yang ditolak.
var parseErrors = map[error]string{
	ErrFileEmpty:    "Pilih file Excel yang akan diimpor.",
	ErrFileTooLarge: "File terlalu besar. Maksimal 5 MB.",
	ErrNotXLSX:      "File harus berformat Excel .xlsx. Buka file di Excel, simpan sebagai .xlsx tanpa password, lalu unggah lagi.",
	ErrNoHeader:     "Sheet pertama kosong. Isi data di sheet pertama dengan judul kolom di baris pertama.",
	ErrNoData:       "Belum ada data siswa di file. Isi data mulai baris kedua.",
	ErrTooManyRows:  "File berisi lebih dari 5.000 baris data. Bagi menjadi beberapa file, lalu unggah satu per satu.",
}

// parseMessage mengembalikan pesan untuk pengguna, atau "" jika err bukan
// kesalahan isi file.
func parseMessage(err error) string {
	var missing *MissingColumnsError
	if errors.As(err, &missing) {
		return missing.Message() + ". Gunakan judul kolom sesuai template."
	}
	var dup *DuplicateColumnError
	if errors.As(err, &dup) {
		return dup.Message() + ". Hapus salah satunya."
	}
	for target, msg := range parseErrors {
		if errors.Is(err, target) {
			return msg
		}
	}
	return ""
}

// upload membaca file unggahan, menolak file yang salah format, lalu
// menyimpan hasil parsing sebagai batch berstatus diunggah.
func (h *Handler) upload(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	file, hdr, err := r.FormFile("file")
	var (
		mbe  *http.MaxBytesError
		rows Rows
	)
	switch {
	case errors.As(err, &mbe):
		err = ErrFileTooLarge
	case errors.Is(err, http.ErrMissingFile):
		err = ErrFileEmpty
	case err != nil:
		h.renderIndex(w, r, id, http.StatusBadRequest, "Unggahan tidak bisa dibaca. Coba lagi.")
		return
	default:
		defer file.Close()
		switch {
		case hdr.Size > MaxFileSize:
			err = ErrFileTooLarge
		case !strings.EqualFold(path.Ext(strings.TrimSpace(hdr.Filename)), ".xlsx"):
			err = ErrNotXLSX
		default:
			rows, err = ParseFile(file)
		}
	}
	if err != nil {
		if msg := parseMessage(err); msg != "" {
			h.logger.InfoContext(r.Context(), "import file rejected", "school_id", id.SchoolID, "user_id", id.UserID, "reason", err.Error())
			h.renderIndex(w, r, id, http.StatusUnprocessableEntity, msg)
			return
		}
		page.ServerError(h.render, h.logger, w, r, id, "parse import file", err)
		return
	}
	batchID, err := h.svc.Upload(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, hdr.Filename, rows, h.today())
	if errors.Is(err, ErrNoActiveYear) {
		h.renderIndex(w, r, id, http.StatusUnprocessableEntity, "Belum ada tahun ajaran aktif, jadi kelas tidak bisa dicocokkan. Aktifkan tahun ajaran di menu Tahun Ajaran, lalu unggah lagi.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "upload import batch", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "File berhasil diunggah dan diperiksa.")
	http.Redirect(w, r, batchPath+"/"+batchID, http.StatusSeeOther)
}

// maxSkippedShown adalah jumlah baris dilewati yang dirinci di halaman batch.
const maxSkippedShown = 20

// maxErrorsShown adalah jumlah baris error yang dirinci di halaman batch.
const maxErrorsShown = 50

type batchView struct {
	User        auth.Identity
	Batch       Batch
	Columns     [NumColumns]Column
	Uploaded    string
	StatusLabel string
	Skipped     []Skip
	SkippedMore int
	Errors      []ErrorRow
	ErrorsMore  int
	Failed      []ErrorRow
	FailedMore  int
	Summary     []KindCount
	SyncLimit   int
	Flash       string
	Error       string
}

var statusLabels = map[string]string{
	StatusUploaded: "Diunggah", StatusValidated: "Divalidasi", StatusRunning: "Berjalan", StatusDone: "Selesai", StatusFailed: "Gagal",
}

func (h *Handler) batch(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderBatch(w, r, id, chi.URLParam(r, "id"), http.StatusOK, "")
}

func (h *Handler) renderBatch(w http.ResponseWriter, r *http.Request, id auth.Identity, batchID string, status int, errMsg string) {
	b, err := h.svc.GetBatch(r.Context(), id.SchoolID, batchID)
	if errors.Is(err, ErrBatchNotFound) {
		page.NotFound(h.render, w, r, id, "Batch impor tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "get import batch", err)
		return
	}
	v := batchView{User: id, Batch: b, Columns: StudentColumns, StatusLabel: statusLabels[b.Status], Skipped: b.Skipped,
		Uploaded:  clock.FormatDateID(clock.DateOf(b.CreatedAt)) + " " + b.CreatedAt.In(clock.Jakarta).Format("15.04") + " WIB",
		SyncLimit: SyncLimit, Error: errMsg}
	if errMsg == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	if b.ErrorRows > 0 {
		if v.Errors, err = h.svc.ErrorRows(r.Context(), id.SchoolID, b.ID, maxErrorsShown); err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "list import error rows", err)
			return
		}
		v.ErrorsMore = b.ErrorRows - len(v.Errors)
	}
	if b.ErrorRows > 0 || b.FailedCount > 0 {
		if v.Summary, err = h.svc.ErrorSummary(r.Context(), id.SchoolID, b.ID); err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "import error summary", err)
			return
		}
	}
	if b.FailedCount > 0 {
		if v.Failed, err = h.svc.FailedRows(r.Context(), id.SchoolID, b.ID, maxErrorsShown); err != nil {
			page.ServerError(h.render, h.logger, w, r, id, "list import failed rows", err)
			return
		}
		v.FailedMore = b.FailedCount - len(v.Failed)
	}
	if n := len(v.Skipped); n > maxSkippedShown {
		v.Skipped, v.SkippedMore = v.Skipped[:maxSkippedShown], n-maxSkippedShown
	}
	h.render.Render(w, r, status, "importer/batch", v)
}

// run menyimpan baris valid batch ke data siswa.
func (h *Handler) run(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	batchID := chi.URLParam(r, "id")
	err := h.svc.Run(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, batchID, h.today())
	switch {
	case errors.Is(err, ErrBatchNotFound):
		page.NotFound(h.render, w, r, id, "Batch impor tidak ditemukan.")
		return
	case errors.Is(err, ErrNotRunnable):
		h.renderBatch(w, r, id, batchID, http.StatusConflict, "Batch ini sudah disimpan atau sedang berjalan. Untuk mengimpor lagi, unggah file sebagai batch baru.")
		return
	case errors.Is(err, ErrNothingToImport):
		h.renderBatch(w, r, id, batchID, http.StatusUnprocessableEntity, "Tidak ada baris yang siap disimpan. Perbaiki file, lalu unggah lagi.")
		return
	case err != nil:
		page.ServerError(h.render, h.logger, w, r, id, "run import batch", err)
		return
	}
	b, err := h.svc.GetBatch(r.Context(), id.SchoolID, batchID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "get import batch", err)
		return
	}
	msg := "Impor berjalan di latar belakang. Halaman ini diperbarui otomatis."
	if b.Status == StatusDone {
		msg = fmt.Sprintf("Impor selesai: %d siswa baru, %d diperbarui, %d dilewati.", b.CreatedCount, b.UpdatedCount, b.SkippedRows)
		if b.FailedCount > 0 {
			msg += fmt.Sprintf(" %d baris gagal disimpan.", b.FailedCount)
		}
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, batchPath+"/"+batchID, http.StatusSeeOther)
}

// columnRow adalah satu kolom template beserta huruf kolomnya di Excel.
type columnRow struct {
	Letter string
	Column
}

func (h *Handler) template(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	data, err := StudentTemplate()
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "student import template", err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	hdr.Set("Content-Disposition", `attachment; filename="`+templateFilename+`"`)
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// errorReport mengirim file Excel berisi baris yang perlu diperbaiki.
func (h *Handler) errorReport(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	batchID := chi.URLParam(r, "id")
	data, err := h.svc.ErrorReport(r.Context(), id.SchoolID, batchID)
	if errors.Is(err, ErrBatchNotFound) {
		page.NotFound(h.render, w, r, id, "Batch impor tidak ditemukan.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "import error report", err)
		return
	}
	hdr := w.Header()
	hdr.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	hdr.Set("Content-Disposition", `attachment; filename="`+errorReportFilename(batchID)+`"`)
	hdr.Set("Cache-Control", "private, no-store")
	hdr.Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// errorReportFilename memberi nama file laporan kesalahan, mis.
// "kesalahan-impor-1a2b3c4d.xlsx" (8 karakter awal id batch).
func errorReportFilename(batchID string) string {
	return "kesalahan-impor-" + batchID[:8] + ".xlsx"
}
