package admission

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/dapodik/ref"
	"akra/internal/platform/clock"
	"akra/internal/platform/money"
	"akra/internal/platform/page"
	"akra/internal/students"
)

const (
	applicantsPath   = "/pendaftaran"
	msgApplicantGone = "Pendaftar yang Anda cari tidak ada atau bukan milik sekolah Anda."
)

type applicantListView struct {
	User     auth.Identity
	CanEdit  bool
	Rows     []applicantRow
	Periods  []option
	Tracks   []option
	Statuses []option
	Kinds    []option
	Query    string
	Filtered bool
	Flash    string
	Error    string
}

type applicantRow struct {
	ListRow
	StatusLabel string
	Submitted   string
}

func (h *Handler) listApplicants(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	q := r.URL.Query()
	f := ListFilter{PeriodID: q.Get("periode"), TrackID: q.Get("jalur"), Status: q.Get("status"), Kind: q.Get("jenis"), Query: q.Get("q")}
	if _, ok := StatusLabels[f.Status]; !ok {
		f.Status = ""
	}
	if f.Kind != KindNew && f.Kind != KindTransfer {
		f.Kind = ""
	}
	periods, err := h.periods.List(r.Context(), id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list periods", err)
		return
	}
	view := applicantListView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Query: f.Query,
		Filtered: f.PeriodID != "" || f.TrackID != "" || f.Status != "" || f.Kind != "" || strings.TrimSpace(f.Query) != "",
		Kinds:    []option{{Value: KindNew, Label: KindLabel(KindNew), Selected: f.Kind == KindNew}, {Value: KindTransfer, Label: KindLabel(KindTransfer), Selected: f.Kind == KindTransfer}},
		Flash:    auth.PopFlash(r.Context(), h.sessions)}
	for _, p := range periods {
		view.Periods = append(view.Periods, option{Value: p.ID, Label: p.Name + " (" + p.AcademicYearName + ")", Selected: p.ID == f.PeriodID})
		if p.ID == f.PeriodID {
			full, err := h.periods.Get(r.Context(), id.SchoolID, p.ID)
			if err != nil {
				h.serverError(w, r, "get period", err)
				return
			}
			for _, t := range full.Tracks {
				view.Tracks = append(view.Tracks, option{Value: t.ID, Label: t.Name, Selected: t.ID == f.TrackID})
			}
		}
	}
	for _, s := range ApplicantStatuses {
		view.Statuses = append(view.Statuses, option{Value: s, Label: StatusLabels[s], Selected: s == f.Status})
	}
	rows, err := h.applicants.List(r.Context(), id.SchoolID, f)
	if err != nil {
		h.serverError(w, r, "list applicants", err)
		return
	}
	for _, x := range rows {
		row := applicantRow{ListRow: x, StatusLabel: StatusLabels[x.Status]}
		if x.SubmittedAt != nil {
			row.Submitted = clock.FormatDateID(*x.SubmittedAt)
		}
		view.Rows = append(view.Rows, row)
	}
	h.render.Render(w, r, http.StatusOK, "admission/applicants", view)
}

type labelValue struct {
	Label, Value string
}

type displaySection struct {
	Title string
	Rows  []labelValue
}

type applicantDetailView struct {
	User        auth.Identity
	CanEdit     bool
	Applicant   Applicant
	StatusLabel string
	Period      Period
	Track       Track
	Sections    []displaySection
	Documents   []Document
	MissingDocs []string
	Events      []eventRow
	Next        []option
	FeeAmount   string
	FeePayments []FeePayment
	FeePaid     bool
	Note        string
	Errors      map[string]string
	ResumeCode  string
	Flash       string
	Error       string
}

type eventRow struct {
	Event
	FromLabel, ToLabel, At string
}

func (h *Handler) applicantDetail(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderApplicant(w, r, id, chi.URLParam(r, "id"), http.StatusOK, "", nil, "")
}

func (h *Handler) renderApplicant(w http.ResponseWriter, r *http.Request, id auth.Identity, applicantID string, status int, note string, errs map[string]string, msg string) {
	ctx := r.Context()
	a, err := h.applicants.Load(ctx, id.SchoolID, applicantID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgApplicantGone)
		return
	}
	if err != nil {
		h.serverError(w, r, "load applicant", err)
		return
	}
	p, err := h.periods.Get(ctx, id.SchoolID, a.PeriodID)
	if err != nil {
		h.serverError(w, r, "get period", err)
		return
	}
	docs, err := h.documents.List(ctx, id.SchoolID, a.ID)
	if err != nil {
		h.serverError(w, r, "list documents", err)
		return
	}
	events, err := h.applicants.Events(ctx, id.SchoolID, a.ID)
	if err != nil {
		h.serverError(w, r, "list events", err)
		return
	}
	view := applicantDetailView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Applicant: a, StatusLabel: StatusLabels[a.Status],
		Period: p, Documents: docs, Note: note, Errors: errs, Error: msg,
		Flash: auth.PopFlash(ctx, h.sessions), ResumeCode: h.sessions.PopString(ctx, sessionStaffCode)}
	for _, t := range p.Tracks {
		if t.ID == a.TrackID {
			view.Track = t
		}
	}
	for _, k := range MissingDocs(p, docs) {
		view.MissingDocs = append(view.MissingDocs, DocLabel(k))
	}
	for _, e := range events {
		view.Events = append(view.Events, eventRow{Event: e, FromLabel: StatusLabels[e.From], ToLabel: StatusLabels[e.To], At: clock.FormatDateID(e.At)})
	}
	for _, s := range NextStatuses(a.Status) {
		view.Next = append(view.Next, option{Value: s, Label: StatusLabels[s]})
	}
	view.Sections = displayApplicant(a)
	var feeAmount int64
	if err := h.db.QueryRow(ctx, `SELECT fee_amount FROM applicants WHERE id = $1`, a.ID).Scan(&feeAmount); err != nil {
		h.serverError(w, r, "fee amount", err)
		return
	}
	if feeAmount > 0 {
		view.FeeAmount = money.FormatRupiah(feeAmount)
		if view.FeePayments, err = h.fees().History(ctx, id.SchoolID, a.ID); err != nil {
			h.serverError(w, r, "fee history", err)
			return
		}
		for _, p := range view.FeePayments {
			view.FeePaid = view.FeePaid || (p.VoidedAt == nil && p.Amount == feeAmount)
		}
	}
	h.render.Render(w, r, status, "admission/applicant_detail", view)
}

// displayApplicant menyusun data pendaftar untuk ditampilkan, dengan kode
// referensi diubah ke label Dapodik. Halaman ini hanya untuk admin_tu dan
// kepala_sekolah, yang berhak melihat nomor identitas utuh.
func displayApplicant(a Applicant) []displaySection {
	gender := map[string]string{"L": "Laki-laki", "P": "Perempuan"}[a.Gender]
	out := []displaySection{{Title: "Identitas", Rows: []labelValue{
		{"Nama lengkap", a.FullName}, {"Jenis kelamin", gender}, {"Tanggal lahir", clock.FormatDateID(a.BirthDate)},
		{"Nomor HP orang tua", a.ContactPhone},
	}}}
	titles := map[string]string{
		students.SectionIdentity: "Data pribadi", students.SectionAddress: "Alamat", students.SectionFamily: "Keluarga", students.SectionAssistance: "Bantuan pemerintah",
		students.SectionContact: "Kontak", students.SectionPeriodic: "Data periodik", students.SectionRegistration: "Sekolah asal",
	}
	for _, sec := range []string{students.SectionIdentity, students.SectionAddress, students.SectionFamily, students.SectionAssistance, students.SectionContact, students.SectionRegistration} {
		ds := displaySection{Title: titles[sec]}
		for _, f := range students.ProfileFields {
			if f.Section != sec {
				continue
			}
			if f.Key == "wilayah_kode" {
				// Alamat tanpa kode wilayah: diisi manual, perlu diperiksa TU.
				if a.Values["wilayah_kode"] == "" && a.Values["village"] != "" {
					ds.Rows = append(ds.Rows, labelValue{"Wilayah", "Desa/kecamatan diisi manual, periksa ejaannya"})
				}
				continue
			}
			if v := displayValue(f.Ref, a.Values[f.Key]); v != "" && v != "false" {
				if v == "true" {
					v = "Ya"
				}
				ds.Rows = append(ds.Rows, labelValue{f.Label, v})
			}
		}
		if sec == students.SectionRegistration && a.Kind == KindTransfer {
			ds.Title = "Sekolah asal (pindahan)"
			for _, k := range transferKeys {
				v := a.Transfer[k]
				if v == "" {
					continue
				}
				switch k {
				case TransferLastGrade, TransferTargetGrade:
					v = "Kelas " + v
				case TransferEntryDate:
					if d, ok := plannedEntry(a); ok {
						v = clock.FormatDateID(d)
					}
				}
				ds.Rows = append(ds.Rows, labelValue{transferLabels[k], v})
			}
		}
		if len(ds.Rows) > 0 {
			out = append(out, ds)
		}
	}
	gtitles := map[string]string{students.RelationFather: "Ayah kandung", students.RelationMother: "Ibu kandung", students.RelationGuardian: "Wali"}
	for _, role := range students.ParentRoles {
		g := a.Guardians[role]
		if g == nil || g["full_name"] == "" {
			continue
		}
		ds := displaySection{Title: gtitles[role]}
		for _, x := range []struct {
			label, key string
			list       *ref.List
		}{
			{"Nama", "full_name", nil}, {"Nomor HP", "phone", nil}, {"NIK", "nik", nil}, {"Tahun lahir", "birth_year", nil},
			{"Pendidikan", "education", &ref.Pendidikan}, {"Pekerjaan", "occupation", &ref.Pekerjaan},
			{"Penghasilan", "income", &ref.Penghasilan}, {"Kebutuhan khusus", "special_needs", &ref.KebutuhanKhusus},
		} {
			if v := displayValue(x.list, g[x.key]); v != "" {
				ds.Rows = append(ds.Rows, labelValue{x.label, v})
			}
		}
		out = append(out, ds)
	}
	return out
}

// displayValue mengubah kode (atau daftar kode dipisah koma) ke label.
func displayValue(l *ref.List, v string) string {
	if l == nil || v == "" {
		return v
	}
	var labels []string
	for _, c := range strings.Split(v, ",") {
		if lbl, err := l.Label(c); err == nil {
			labels = append(labels, lbl)
		} else {
			labels = append(labels, c)
		}
	}
	return strings.Join(labels, ", ")
}

const sessionStaffCode = "admission_staff_code"

func (h *Handler) setApplicantStatus(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	applicantID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxPeriodForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	to, note := r.PostForm.Get("status"), r.PostForm.Get("note")
	res, err := h.applicants.Transition(r.Context(), actorFrom(r, id), applicantID, to, note)
	var ve *ValidationError
	switch {
	case err == nil:
		msg := "Status " + res.Applicant.FullName + " menjadi " + strings.ToLower(StatusLabels[to]) + "."
		if res.OverQuota {
			msg += " Perhatian: jumlah diterima di jalur ini sudah melebihi kuota."
		}
		auth.SetFlash(r.Context(), h.sessions, msg)
		if res.ResumeCode != "" {
			h.sessions.Put(r.Context(), sessionStaffCode, res.ResumeCode)
		}
		http.Redirect(w, r, applicantsPath+"/"+applicantID, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgApplicantGone)
	case errors.As(err, &ve):
		h.renderApplicant(w, r, id, applicantID, http.StatusBadRequest, note, ve.Fields, "")
	case errors.Is(err, ErrBadTransition):
		h.renderApplicant(w, r, id, applicantID, http.StatusConflict, note, nil, "Status tidak bisa diubah ke pilihan itu. Muat ulang halaman; mungkin petugas lain sudah mengubahnya.")
	case errors.Is(err, ErrFeeUnpaid):
		h.renderApplicant(w, r, id, applicantID, http.StatusConflict, note, nil, "Biaya formulir belum lunas. Periode ini mewajibkan lunas sebelum verifikasi.")
	default:
		h.serverError(w, r, "set applicant status", err)
	}
}

// openDocument mengalirkan dokumen yang sudah didekripsi. Peran: admin_tu,
// kepala_sekolah. Gambar dibuka inline, PDF diunduh.
func (h *Handler) openDocument(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	d, body, err := h.documents.Open(r.Context(), actorFrom(r, id), chi.URLParam(r, "id"), chi.URLParam(r, "docID"))
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, "Dokumen tidak ditemukan.")
		return
	}
	if err != nil {
		h.serverError(w, r, "open document", err)
		return
	}
	disposition := "inline"
	if d.ContentType == "application/pdf" {
		disposition = "attachment"
	}
	name := d.DocType + map[string]string{"image/jpeg": ".jpg", "image/png": ".png", "application/pdf": ".pdf"}[d.ContentType]
	w.Header().Set("Content-Type", d.ContentType)
	w.Header().Set("Content-Disposition", disposition+`; filename="`+name+`"; filename*=UTF-8''`+url.PathEscape(name))
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(body)
}
