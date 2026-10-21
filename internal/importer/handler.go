package importer

import (
	"errors"
	"fmt"
	"log/slog"
	"mime/multipart"
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
	gradesPath       = "/impor/nilai"
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
		// Peran: admin_tu. Impor nilai lewat mapping (T82).
		r.Get(gradesPath, h.gradesIndex)
		// Peran: admin_tu.
		r.Post(gradesPath, h.gradesUpload)
	})
}

type indexView struct {
	User    auth.Identity
	Columns []columnRow
	// Mappings adalah mapping sekolah untuk target halaman ini (T82);
	// MappingID pilihan terakhir.
	Mappings  []Mapping
	MappingID string
	Filename  string
	MaxBytes  int
	Flash     string
	Error     string
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderIndex(w, r, id, http.StatusOK, "")
}

func (h *Handler) renderIndex(w http.ResponseWriter, r *http.Request, id auth.Identity, status int, errMsg string) {
	h.renderTargetIndex(w, r, id, TargetStudents, "importer/index", status, errMsg, r.FormValue("mapping_id"))
}

// renderTargetIndex menampilkan halaman unggah target (siswa atau nilai)
// beserta mapping sekolah untuk target itu.
func (h *Handler) renderTargetIndex(w http.ResponseWriter, r *http.Request, id auth.Identity, target, pageName string, status int, errMsg, mappingID string) {
	t, _ := TargetByKey(target)
	rows := make([]columnRow, len(t.Columns))
	for i, c := range t.Columns {
		rows[i] = columnRow{Letter: string(rune('A' + i)), Column: c}
	}
	all, err := NewMappingService(h.svc.db).List(r.Context(), id.SchoolID)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "list import mappings", err)
		return
	}
	v := indexView{User: id, Columns: rows, Filename: templateFilename, MaxBytes: MaxFileSize, Error: errMsg, MappingID: mappingID}
	for _, m := range all {
		if m.Target == target {
			v.Mappings = append(v.Mappings, m)
		}
	}
	if errMsg == "" {
		v.Flash = auth.PopFlash(r.Context(), h.sessions)
	}
	h.render.Render(w, r, status, pageName, v)
}

// formFile membaca file unggahan dengan batas ukuran dan jenis yang sama
// untuk semua target. Pemanggil menutup file.
func formFile(w http.ResponseWriter, r *http.Request) (multipart.File, *multipart.FileHeader, error) {
	r.Body = http.MaxBytesReader(w, r.Body, maxUpload)
	file, hdr, err := r.FormFile("file")
	var mbe *http.MaxBytesError
	switch {
	case errors.As(err, &mbe):
		return nil, nil, ErrFileTooLarge
	case errors.Is(err, http.ErrMissingFile):
		return nil, nil, ErrFileEmpty
	case err != nil:
		return nil, nil, err
	}
	switch {
	case hdr.Size > MaxFileSize:
		file.Close()
		return nil, nil, ErrFileTooLarge
	case !strings.EqualFold(path.Ext(strings.TrimSpace(hdr.Filename)), ".xlsx"):
		file.Close()
		return nil, nil, ErrNotXLSX
	}
	return file, hdr, nil
}

// mappingFor mengembalikan mapping mappingID milik sekolah untuk target;
// kosong = judul kolom baku. ok false jika mapping tidak ada, milik sekolah
// lain, atau untuk target lain.
func (h *Handler) mappingFor(r *http.Request, schoolID, mappingID, target string) (Mapping, bool, error) {
	if mappingID == "" {
		return StandardMapping(target), true, nil
	}
	m, err := NewMappingService(h.svc.db).Get(r.Context(), schoolID, mappingID)
	if errors.Is(err, ErrMappingNotFound) {
		return Mapping{}, false, nil
	}
	if err != nil {
		return Mapping{}, false, err
	}
	return m, m.Target == target, nil
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
	var src *MappingSourceError
	if errors.As(err, &src) {
		return src.Message()
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
	file, hdr, err := formFile(w, r)
	if err != nil {
		if msg := parseMessage(err); msg != "" {
			h.renderIndex(w, r, id, http.StatusUnprocessableEntity, msg)
			return
		}
		h.renderIndex(w, r, id, http.StatusBadRequest, "Unggahan tidak bisa dibaca. Coba lagi.")
		return
	}
	var rows Rows
	mappingID := r.FormValue("mapping_id")
	{
		defer file.Close()
		// Tanpa mapping: judul kolom template (T53). Dengan mapping: judul
		// kolom file sekolah dipetakan ke kolom baku (T82), lalu validasi T54
		// yang sama.
		if mappingID == "" {
			rows, err = ParseFile(file)
		} else {
			mp, ok, merr := h.mappingFor(r, id.SchoolID, mappingID, TargetStudents)
			switch {
			case merr != nil:
				err = merr
			case !ok:
				h.renderIndex(w, r, id, http.StatusUnprocessableEntity, "Mapping tidak ditemukan. Pilih mapping dari daftar.")
				return
			default:
				var mapped MappedRows
				if mapped, err = ParseMapped(file, mp); err == nil {
					rows, err = mapped.StudentRows()
				}
			}
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
	batchID, err := h.svc.UploadMapped(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, hdr.Filename, rows, mappingID, h.today())
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

func (h *Handler) gradesIndex(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderTargetIndex(w, r, id, TargetGrades, "importer/grades", http.StatusOK, "", r.URL.Query().Get("mapping_id"))
}

// gradesUpload membaca file nilai lewat mapping (kosong = judul kolom baku),
// lalu menyimpan dan memvalidasinya sebagai batch nilai (T82).
func (h *Handler) gradesUpload(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	fail := func(status int, msg string) {
		h.renderTargetIndex(w, r, id, TargetGrades, "importer/grades", status, msg, r.FormValue("mapping_id"))
	}
	file, hdr, err := formFile(w, r)
	if err != nil {
		if msg := parseMessage(err); msg != "" {
			fail(http.StatusUnprocessableEntity, msg)
			return
		}
		fail(http.StatusBadRequest, "Unggahan tidak bisa dibaca. Coba lagi.")
		return
	}
	defer file.Close()
	mappingID := r.FormValue("mapping_id")
	mp, ok, err := h.mappingFor(r, id.SchoolID, mappingID, TargetGrades)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "get import mapping", err)
		return
	}
	if !ok {
		fail(http.StatusUnprocessableEntity, "Mapping tidak ditemukan. Pilih mapping dari daftar.")
		return
	}
	mapped, err := ParseMapped(file, mp)
	if err != nil {
		if msg := parseMessage(err); msg != "" {
			fail(http.StatusUnprocessableEntity, msg)
			return
		}
		page.ServerError(h.render, h.logger, w, r, id, "parse grade import", err)
		return
	}
	batchID, err := h.svc.UploadGrades(r.Context(), school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}, hdr.Filename, mapped, mappingID, h.today())
	if errors.Is(err, ErrNoActiveYear) {
		fail(http.StatusUnprocessableEntity, "Belum ada tahun ajaran aktif. Aktifkan tahun ajaran di menu Tahun Ajaran, lalu unggah lagi.")
		return
	}
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "upload grade import", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "File nilai berhasil diunggah dan diperiksa.")
	http.Redirect(w, r, batchPath+"/"+batchID, http.StatusSeeOther)
}

// maxSkippedShown adalah jumlah baris dilewati yang dirinci di halaman batch.
const maxSkippedShown = 20

// maxErrorsShown adalah jumlah baris error yang dirinci di halaman batch.
const maxErrorsShown = 50

type batchView struct {
	User        auth.Identity
	Batch       Batch
	Columns     []Column
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
	v := batchView{User: id, Batch: b, Columns: b.Columns(), StatusLabel: statusLabels[b.Status], Skipped: b.Skipped,
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
		what := "siswa"
		if b.Grades() {
			what = "nilai"
		}
		msg = fmt.Sprintf("Impor selesai: %d %s baru, %d diperbarui, %d dilewati.", b.CreatedCount, what, b.UpdatedCount, b.SkippedRows)
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
