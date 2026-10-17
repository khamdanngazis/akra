package students

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/platform/clock"
	"akra/internal/platform/page"
	"akra/internal/school"
)

const (
	studentsPath      = "/siswa"
	maxStudentForm    = 16 << 10
	msgStudentMissing = "Siswa yang Anda cari tidak ada atau bukan milik sekolah Anda."
)

// Handler melayani halaman data siswa.
type Handler struct {
	svc       *Service
	guardians *GuardianService
	accounts  *GuardianAccountService
	sessions  *scs.SessionManager
	render    page.Renderer
	logger    *slog.Logger
}

// NewHandler mengembalikan Handler. Route-nya harus berada di dalam grup
// RequireLogin.
func NewHandler(svc *Service, guardians *GuardianService, accounts *GuardianAccountService, sessions *scs.SessionManager, render page.Renderer, logger *slog.Logger) *Handler {
	return &Handler{svc: svc, guardians: guardians, accounts: accounts, sessions: sessions, render: render, logger: logger}
}

// Routes mendaftarkan route siswa ke r. Guru dan orang tua tidak punya akses
// ke daftar siswa (guru lewat absensi, T26; orang tua lewat portal, T43).
func (h *Handler) Routes(r chi.Router) {
	r.Route(studentsPath, func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU, auth.RoleKepalaSekolah))
			// Peran: admin_tu, kepala_sekolah (hanya baca).
			r.Get("/", h.index)
			// Peran: admin_tu, kepala_sekolah (hanya baca).
			r.Get("/{id}", h.detail)
		})
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireRole(auth.RoleAdminTU))
			// Peran: admin_tu.
			r.Get("/baru", h.newForm)
			// Peran: admin_tu.
			r.Post("/", h.create)
			// Peran: admin_tu.
			r.Get("/{id}/ubah", h.editForm)
			// Peran: admin_tu.
			r.Post("/{id}", h.update)
			// Peran: admin_tu.
			r.Post("/{id}/pindah-kelas", h.move)
			// Peran: admin_tu.
			r.Post("/{id}/wali/{guardianID}/akun", h.createAccount)
			// Peran: admin_tu.
			r.Post("/{id}/wali/{guardianID}/akun/kirim-ulang", h.resendActivation)
		})
	})
}

type option struct {
	Value, Label string
	Selected     bool
}

type indexView struct {
	User       auth.Identity
	CanEdit    bool
	Flash      string
	Error      string
	Query      string
	Years      []option
	Classes    []option
	Statuses   []option
	YearName   string
	Page       Page
	Rows       []ListRow
	From, To   int
	PrevURL    string
	NextURL    string
	HasFilters bool
}

type formView struct {
	User     auth.Identity
	IsNew    bool
	ID       string
	Form     Form
	YearName string   // tahun aktif, saat membuat
	Classes  []string // nama kelas tahun aktif, untuk datalist
	Statuses []option
	Errors   map[string]string
	Error    string
	Flash    string
}

type detailView struct {
	User      auth.Identity
	CanEdit   bool
	Student   Student
	History   []historyRow
	Guardians []guardianRow
	YearName  string
	Classes   []string
	Move      MoveForm
	Errors    map[string]string
	Error     string
	Flash     string
}

type guardianRow struct {
	ID            string
	FullName      string
	Relationship  string
	IsPrimary     bool
	Phone         string
	Email         string
	AccountStatus string
}

var relationshipLabels = map[string]string{RelationFather: "Ayah", RelationMother: "Ibu", RelationGuardian: "Wali", RelationOther: "Lainnya"}

type historyRow struct {
	ClassName string
	Start     string
	End       string // kosong jika masih aktif
	Active    bool
	Cancelled bool // dibatalkan di hari yang sama
}

func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	years, err := school.ListAcademicYears(r.Context(), h.svc.db, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list academic years", err)
		return
	}

	f := ListFilter{
		Query:  strings.TrimSpace(q.Get("q")),
		Status: q.Get("status"),
	}
	f.Page, _ = strconv.Atoi(q.Get("page"))
	if !contains(Statuses, f.Status) {
		f.Status = ""
	}
	year := pickYear(years, q.Get("tahun"))
	view := indexView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Flash: auth.PopFlash(r.Context(), h.sessions), Query: f.Query}
	if year != nil {
		f.AcademicYearID = year.ID
		view.YearName = year.Name
		classList, err := classes.ListByYear(r.Context(), h.svc.db, id.SchoolID, year.ID)
		if err != nil {
			h.serverError(w, r, "list classes", err)
			return
		}
		for _, c := range classList {
			if c.ID == q.Get("kelas") {
				f.ClassID = c.ID
			}
			view.Classes = append(view.Classes, option{Value: c.ID, Label: c.Name, Selected: c.ID == q.Get("kelas")})
		}
	}
	for _, y := range years {
		label := y.Name
		if y.IsActive {
			label += " (aktif)"
		}
		view.Years = append(view.Years, option{Value: y.ID, Label: label, Selected: year != nil && y.ID == year.ID})
	}
	view.Statuses = statusOptions(f.Status, "Semua status")

	pg, err := h.svc.List(r.Context(), id.SchoolID, f)
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	view.Page, view.Rows = pg, pg.Rows
	view.HasFilters = f.Query != "" || f.ClassID != "" || f.Status != ""
	if pg.Total > 0 && len(pg.Rows) > 0 {
		view.From = (pg.Page-1)*PageSize + 1
		view.To = view.From + len(pg.Rows) - 1
	}
	base := url.Values{}
	for k, v := range map[string]string{"q": f.Query, "tahun": f.AcademicYearID, "kelas": f.ClassID, "status": f.Status} {
		if v != "" {
			base.Set(k, v)
		}
	}
	if pg.Page > 1 {
		view.PrevURL = pageURL(base, pg.Page-1)
	}
	if pg.Page < pg.LastPage {
		view.NextURL = pageURL(base, pg.Page+1)
	}
	h.render.Render(w, r, http.StatusOK, "students/index", view)
}

func (h *Handler) detail(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderDetail(w, r, id, chi.URLParam(r, "id"), http.StatusOK, MoveForm{}, nil)
}

func (h *Handler) renderDetail(w http.ResponseWriter, r *http.Request, id auth.Identity, studentID string, status int, move MoveForm, errs map[string]string) {
	st, err := h.svc.Get(r.Context(), id.SchoolID, studentID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgStudentMissing)
		return
	}
	if err != nil {
		h.serverError(w, r, "get student", err)
		return
	}
	history, err := h.svc.History(r.Context(), id.SchoolID, st.ID)
	if err != nil {
		h.serverError(w, r, "list enrollments", err)
		return
	}
	view := detailView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Student: st, Move: move, Errors: errs, Flash: auth.PopFlash(r.Context(), h.sessions)}
	if view.Move.Date == "" {
		view.Move.Date = clock.Today().Format(dateLayout)
	}
	for _, e := range history {
		row := historyRow{ClassName: e.ClassName, Start: clock.FormatDateID(e.StartDate), Active: e.Active()}
		if e.EndDate != nil {
			row.End = clock.FormatDateID(*e.EndDate)
			row.Cancelled = e.EndDate.Before(e.StartDate)
		}
		view.History = append(view.History, row)
	}
	guardians, err := h.guardians.ListByStudent(r.Context(), id.SchoolID, st.ID)
	if err != nil {
		h.serverError(w, r, "list guardians", err)
		return
	}
	for _, g := range guardians {
		view.Guardians = append(view.Guardians, guardianRow{
			ID: g.ID, FullName: g.FullName, Relationship: relationshipLabels[g.Relationship], IsPrimary: g.IsPrimary,
			Phone: g.Phone, Email: g.Email, AccountStatus: g.AccountStatus,
		})
	}
	if view.CanEdit {
		if ok := h.fillClasses(w, r, id, &view.YearName, &view.Classes); !ok {
			return
		}
	}
	h.render.Render(w, r, status, "students/detail", view)
}

func (h *Handler) newForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	view := formView{User: id, IsNew: true}
	if ok := h.fillClasses(w, r, id, &view.YearName, &view.Classes); !ok {
		return
	}
	h.render.Render(w, r, http.StatusOK, "students/form", view)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	f, ok := parseStudentForm(w, r)
	if !ok {
		return
	}
	st, err := h.svc.Create(r.Context(), actorFrom(r, id), f)
	if err != nil {
		view := formView{User: id, IsNew: true, Form: f}
		if ok := h.fillClasses(w, r, id, &view.YearName, &view.Classes); ok {
			h.formError(w, r, err, view)
		}
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Siswa "+st.FullName+" berhasil ditambahkan.")
	http.Redirect(w, r, studentsPath+"/"+st.ID, http.StatusSeeOther)
}

func (h *Handler) editForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	st, err := h.svc.Get(r.Context(), id.SchoolID, chi.URLParam(r, "id"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgStudentMissing)
		return
	}
	if err != nil {
		h.serverError(w, r, "get student", err)
		return
	}
	f := Form{NIS: st.NIS, FullName: st.FullName, Gender: st.Gender, Status: st.Status}
	if st.BirthDate != nil {
		f.BirthDate = st.BirthDate.Format(dateLayout)
	}
	h.render.Render(w, r, http.StatusOK, "students/form", formView{User: id, ID: st.ID, Form: f, Statuses: statusOptions(st.Status, "")})
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	studentID := chi.URLParam(r, "id")
	f, ok := parseStudentForm(w, r)
	if !ok {
		return
	}
	st, err := h.svc.Update(r.Context(), actorFrom(r, id), studentID, f)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgStudentMissing)
		return
	}
	if err != nil {
		h.formError(w, r, err, formView{User: id, ID: studentID, Form: f, Statuses: statusOptions(f.Status, "")})
		return
	}
	auth.SetFlash(r.Context(), h.sessions, "Data "+st.FullName+" berhasil diubah.")
	http.Redirect(w, r, studentsPath+"/"+st.ID, http.StatusSeeOther)
}

func (h *Handler) move(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	studentID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxStudentForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	f := MoveForm{ClassName: r.PostForm.Get("class_name"), Date: r.PostForm.Get("date")}
	res, err := h.svc.Move(r.Context(), actorFrom(r, id), studentID, f)
	var ve *ValidationError
	switch {
	case err == nil:
		msg := "Siswa dipindahkan ke kelas " + res.Enrollment.ClassName + "."
		if !res.Changed {
			msg = "Siswa sudah berada di kelas " + res.Enrollment.ClassName + "."
		}
		auth.SetFlash(r.Context(), h.sessions, msg)
		http.Redirect(w, r, studentsPath+"/"+studentID, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgStudentMissing)
	case errors.As(err, &ve):
		h.renderDetail(w, r, id, studentID, http.StatusBadRequest, f, ve.Fields)
	default:
		h.serverError(w, r, "move student", err)
	}
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	h.accountAction(w, r, false)
}

func (h *Handler) resendActivation(w http.ResponseWriter, r *http.Request) {
	h.accountAction(w, r, true)
}

// accountAction membuat akun portal wali, atau mengirim ulang tautan
// aktivasinya. Wali harus terhubung ke siswa di URL.
func (h *Handler) accountAction(w http.ResponseWriter, r *http.Request, resend bool) {
	id, _ := auth.CurrentUser(r.Context())
	studentID, guardianID := chi.URLParam(r, "id"), chi.URLParam(r, "guardianID")
	guardians, err := h.guardians.ListByStudent(r.Context(), id.SchoolID, studentID)
	if err != nil {
		h.serverError(w, r, "list guardians", err)
		return
	}
	linked := false
	for _, g := range guardians {
		linked = linked || g.ID == guardianID
	}
	if !linked {
		page.NotFound(h.render, w, r, id, "Wali yang Anda pilih tidak terhubung ke siswa ini.")
		return
	}

	var res AccountResult
	if resend {
		res, err = h.accounts.ResendActivation(r.Context(), actorFrom(r, id), guardianID)
	} else {
		res, err = h.accounts.CreateAccount(r.Context(), actorFrom(r, id), guardianID)
	}
	var msg string
	switch {
	case err == nil && res.MailErr != nil:
		msg = "Akun dibuat, tetapi email aktivasi ke " + res.Email + " gagal dikirim. Coba kirim ulang."
	case err == nil && resend:
		msg = "Tautan aktivasi baru dikirim ke " + res.Email + "."
	case err == nil:
		msg = "Akun portal dibuat. Tautan aktivasi dikirim ke " + res.Email + "."
	case errors.Is(err, ErrGuardianNoEmail):
		msg = "Wali belum punya email. Tambahkan email dulu untuk membuat akun portal."
	case errors.Is(err, ErrAccountExists):
		msg = "Wali ini sudah punya akun portal."
	case errors.Is(err, ErrAccountNotPending):
		msg = "Akun wali ini tidak sedang menunggu aktivasi."
	case errors.Is(err, ErrEmailInUse):
		msg = "Email wali sudah dipakai akun lain di sekolah ini."
	case errors.Is(err, ErrGuardianNotFound):
		page.NotFound(h.render, w, r, id, "Wali yang Anda pilih tidak ditemukan.")
		return
	default:
		h.serverError(w, r, "guardian account", err)
		return
	}
	auth.SetFlash(r.Context(), h.sessions, msg)
	http.Redirect(w, r, studentsPath+"/"+studentID, http.StatusSeeOther)
}

// fillClasses mengisi nama tahun aktif dan daftar nama kelasnya (untuk
// datalist). Tanpa tahun aktif, keduanya kosong.
func (h *Handler) fillClasses(w http.ResponseWriter, r *http.Request, id auth.Identity, yearName *string, names *[]string) bool {
	year, list, err := h.svc.ActiveYearClasses(r.Context(), id.SchoolID)
	if errors.Is(err, ErrNoActiveYear) {
		return true
	}
	if err != nil {
		h.serverError(w, r, "list active year classes", err)
		return false
	}
	*yearName = year.Name
	for _, c := range list {
		*names = append(*names, c.Name)
	}
	return true
}

func (h *Handler) formError(w http.ResponseWriter, r *http.Request, err error, view formView) {
	var ve *ValidationError
	if errors.As(err, &ve) {
		view.Errors = ve.Fields
		h.render.Render(w, r, http.StatusBadRequest, "students/form", view)
		return
	}
	h.serverError(w, r, "save student", err)
}

func (h *Handler) serverError(w http.ResponseWriter, r *http.Request, op string, err error) {
	id, _ := auth.CurrentUser(r.Context())
	page.ServerError(h.render, h.logger, w, r, id, op, err)
}

func parseStudentForm(w http.ResponseWriter, r *http.Request) (Form, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxStudentForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return Form{}, false
	}
	p := r.PostForm
	return Form{
		NIS: p.Get("nis"), FullName: p.Get("full_name"), BirthDate: p.Get("birth_date"),
		Gender: p.Get("gender"), Status: p.Get("status"), ClassName: p.Get("class_name"),
	}, true
}

var statusLabels = map[string]string{StatusActive: "Aktif", StatusTransferred: "Pindah", StatusGraduated: "Lulus", StatusLeft: "Keluar"}

// statusOptions menyusun pilihan status. allLabel diisi untuk filter
// ("Semua status"); kosong untuk form.
func statusOptions(selected, allLabel string) []option {
	var opts []option
	if allLabel != "" {
		opts = append(opts, option{Value: "", Label: allLabel, Selected: selected == ""})
	}
	for _, s := range Statuses {
		opts = append(opts, option{Value: s, Label: statusLabels[s], Selected: s == selected})
	}
	return opts
}

// pickYear memilih tahun ajaran dari parameter, lalu tahun aktif, lalu yang
// terbaru. Parameter yang bukan milik sekolah diabaikan.
func pickYear(years []school.AcademicYear, param string) *school.AcademicYear {
	for i := range years {
		if years[i].ID == param {
			return &years[i]
		}
	}
	for i := range years {
		if years[i].IsActive {
			return &years[i]
		}
	}
	if len(years) > 0 {
		return &years[0]
	}
	return nil
}

func pageURL(base url.Values, p int) string {
	v := url.Values{}
	for k, vals := range base {
		v[k] = vals
	}
	v.Set("page", strconv.Itoa(p))
	return studentsPath + "?" + v.Encode()
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func actorFrom(r *http.Request, id auth.Identity) school.Actor {
	return school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
}
