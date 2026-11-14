package students

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"akra/internal/auth"
	"akra/internal/dapodik/ref"
	"akra/internal/platform/page"
)

// Bagian form data rinci, berurutan seperti F-PD (T88). Bagian ayah, ibu,
// dan wali menyimpan ke data wali; bagian lain ke student_profiles.
var profileSections = []struct {
	Key, Title, Hint string
}{
	{SectionIdentity, "Data pribadi", "Nama, NIS, tanggal lahir, dan jenis kelamin diubah di form data siswa."},
	{SectionAddress, "Alamat", ""},
	{SectionFamily, "Keluarga dan bantuan", ""},
	{RelationFather, "Ayah kandung", "Hindari gelar (Alm., Dr., S.Pd, H.)."},
	{RelationMother, "Ibu kandung", "Nama ibu kandung wajib untuk Dapodik. Hindari gelar (Almh., Hj., Dra.)."},
	{RelationGuardian, "Wali", "Isi jika siswa tinggal bersama wali."},
	{SectionContact, "Kontak", ""},
	{SectionPeriodic, "Data periodik", ""},
	{SectionRegistration, "Registrasi", ""},
}

type profileView struct {
	User      auth.Identity
	CanEdit   bool
	Student   Student
	Readiness Readiness
	Sections  []sectionView
	Flash     string
	Error     string
}

type sectionView struct {
	Key, Title, Hint string
	Lower            string // Title huruf kecil, untuk kalimat
	Fields           []FieldView
	// Parent diisi untuk bagian ayah, ibu, dan wali.
	Parent *parentView
	Failed bool // bagian ini gagal disimpan
}

type parentView struct {
	Exists   bool
	FullName string // nama wali yang sudah ada, atau isian form
	Error    string
}

// FieldView adalah satu input di form data rinci; dipakai partial
// "profile_fields" di halaman siswa dan formulir pendaftaran.
type FieldView struct {
	Key, Label string
	// Input adalah jenis input: text, email, number, date, select, multi,
	// atau checkbox.
	Input     string
	InputMode string
	Value     string
	Checked   map[string]bool // untuk multi
	Options   []Option
	Max       int
	Required  bool // wajib untuk Dapodik
	Error     string
}

var requiredKeys = func() map[string]bool {
	m := map[string]bool{}
	for _, f := range DapodikRequired {
		m[f.Key] = true
	}
	return m
}()

// profile menampilkan form data rinci. Peran: admin_tu (ubah),
// kepala_sekolah (baca).
func (h *Handler) profile(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.renderProfile(w, r, id, chi.URLParam(r, "id"), http.StatusOK, "", nil, nil)
}

// saveProfile menyimpan satu bagian form data rinci. Peran: admin_tu.
func (h *Handler) saveProfile(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	studentID := chi.URLParam(r, "id")
	r.Body = http.MaxBytesReader(w, r.Body, maxStudentForm)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	section := r.PostForm.Get("section")
	title := ""
	for _, s := range profileSections {
		if s.Key == section {
			title = s.Title
		}
	}
	if title == "" {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}

	submitted := map[string]string{}
	var err error
	if slices.Contains(ParentRoles, section) {
		submitted["full_name"] = r.PostForm.Get("full_name")
		for _, k := range []string{"nik", "birth_year", "education", "occupation", "income"} {
			submitted[k] = r.PostForm.Get(k)
		}
		submitted["special_needs"] = strings.Join(r.PostForm["special_needs"], ",")
		_, err = h.details.SaveParent(r.Context(), actorFrom(r, id), studentID, section, submitted["full_name"], GuardianDetailsForm{
			NIK: submitted["nik"], BirthYear: submitted["birth_year"], Education: submitted["education"],
			Occupation: submitted["occupation"], Income: submitted["income"], SpecialNeeds: r.PostForm["special_needs"],
		})
	} else {
		for _, f := range ProfileFields {
			if f.Section != section {
				continue
			}
			switch f.Kind {
			case kindBool:
				submitted[f.Key] = strconv.FormatBool(r.PostForm.Has(f.Key))
			case kindRefMulti:
				submitted[f.Key] = strings.Join(r.PostForm[f.Key], ",")
			default:
				submitted[f.Key] = r.PostForm.Get(f.Key)
			}
		}
		_, err = h.profiles.Save(r.Context(), actorFrom(r, id), studentID, submitted)
	}

	var ve *ValidationError
	switch {
	case err == nil:
		auth.SetFlash(r.Context(), h.sessions, "Data "+strings.ToLower(title)+" disimpan.")
		http.Redirect(w, r, studentsPath+"/"+studentID+"/data-rinci#"+section, http.StatusSeeOther)
	case errors.Is(err, ErrNotFound):
		page.NotFound(h.render, w, r, id, msgStudentMissing)
	case errors.As(err, &ve):
		h.renderProfile(w, r, id, studentID, http.StatusBadRequest, section, submitted, ve.Fields)
	default:
		h.serverError(w, r, "save student profile", err)
	}
}

// renderProfile menampilkan form. failed, submitted, dan errs diisi jika
// satu bagian gagal disimpan: isian bagian itu ditampilkan kembali apa
// adanya, bagian lain dari database.
func (h *Handler) renderProfile(w http.ResponseWriter, r *http.Request, id auth.Identity, studentID string, status int, failed string, submitted, errs map[string]string) {
	ctx := r.Context()
	st, err := h.svc.Get(ctx, id.SchoolID, studentID)
	if errors.Is(err, ErrNotFound) {
		page.NotFound(h.render, w, r, id, msgStudentMissing)
		return
	}
	if err != nil {
		h.serverError(w, r, "get student", err)
		return
	}
	prof, err := h.profiles.Get(ctx, id.SchoolID, st.ID)
	if err != nil {
		h.serverError(w, r, "get student profile", err)
		return
	}
	readiness, err := GetReadiness(ctx, h.svc.db, id.SchoolID, st.ID)
	if err != nil {
		h.serverError(w, r, "get readiness", err)
		return
	}
	guardians, err := h.guardians.ListByStudent(ctx, id.SchoolID, st.ID)
	if err != nil {
		h.serverError(w, r, "list guardians", err)
		return
	}

	view := profileView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Student: st, Readiness: readiness,
		Flash: auth.PopFlash(ctx, h.sessions)}
	if failed != "" {
		view.Error = "Data belum disimpan. Periksa isian yang ditandai."
	}
	for _, s := range profileSections {
		sv := sectionView{Key: s.Key, Title: s.Title, Lower: strings.ToLower(s.Title), Hint: s.Hint, Failed: s.Key == failed}
		values := map[string]string{}
		if slices.Contains(ParentRoles, s.Key) {
			sv.Parent = &parentView{}
			if g, ok := FindParent(guardians, s.Key); ok {
				sv.Parent.Exists, sv.Parent.FullName = true, g.FullName
				d, err := h.details.Get(ctx, id.SchoolID, g.ID)
				if err != nil {
					h.serverError(w, r, "get guardian details", err)
					return
				}
				values = guardianValues(d)
			}
			if sv.Failed {
				values = submitted
				if !sv.Parent.Exists {
					sv.Parent.FullName = submitted["full_name"]
				}
				sv.Parent.Error = errs["full_name"]
			}
			sv.Fields = GuardianFieldViews(values, errsFor(sv.Failed, errs))
		} else {
			for _, f := range ProfileFields {
				if f.Section == s.Key {
					// Halaman ini hanya untuk admin_tu dan kepala_sekolah,
					// yang berhak melihat nomor identitas utuh.
					values[f.Key] = prof.Secret(f.Key).Reveal()
				}
			}
			if sv.Failed {
				for k, v := range submitted {
					values[k] = v
				}
			}
			sv.Fields = ProfileFieldViews(s.Key, values, errsFor(sv.Failed, errs))
		}
		view.Sections = append(view.Sections, sv)
	}
	h.render.Render(w, r, status, "students/profile", view)
}

func errsFor(failed bool, errs map[string]string) map[string]string {
	if failed {
		return errs
	}
	return nil
}

// ProfileFieldViews membuat input untuk satu bagian data rinci.
func ProfileFieldViews(section string, values, errs map[string]string) []FieldView {
	var out []FieldView
	for _, f := range ProfileFields {
		if f.Section != section {
			continue
		}
		fv := FieldView{Key: f.Key, Label: f.Label, Value: values[f.Key], Error: errs[f.Key], Required: requiredKeys[f.Key], Input: "text"}
		switch f.Kind {
		case kindText:
			fv.Max = int(f.Max)
		case kindNISN, kindKK, kindDigits:
			fv.InputMode = "numeric"
		case kindPhone:
			fv.InputMode = "tel"
		case kindDecimal:
			fv.InputMode = "decimal"
		case kindEmail:
			fv.Input = "email"
		case kindInt:
			fv.Input = "number"
		case kindDate:
			fv.Input = "date"
		case kindBool:
			fv.Input = "checkbox"
		case kindRef:
			fv.Input = "select"
			fv.Options = refOptions(*f.Ref, fv.Value)
		case kindRefMulti:
			fv.Input = "multi"
			fv.Options = refOptions(*f.Ref, "")
			fv.Checked = checkedSet(fv.Value)
		}
		out = append(out, fv)
	}
	return out
}

func guardianValues(d GuardianDetails) map[string]string {
	v := map[string]string{
		"nik": d.NIK.Reveal(), "education": d.Education, "occupation": d.Occupation, "income": d.Income,
		"special_needs": strings.Join(d.SpecialNeeds, ","),
	}
	if d.BirthYear > 0 {
		v["birth_year"] = strconv.Itoa(d.BirthYear)
	}
	return v
}

// GuardianFieldViews membuat input data rinci wali.
func GuardianFieldViews(values, errs map[string]string) []FieldView {
	return []FieldView{
		{Key: "nik", Label: "NIK", Input: "text", InputMode: "numeric", Value: values["nik"], Error: errs["nik"]},
		{Key: "birth_year", Label: "Tahun lahir", Input: "number", Value: values["birth_year"], Error: errs["birth_year"]},
		{Key: "education", Label: "Pendidikan terakhir", Input: "select", Value: values["education"], Options: refOptions(ref.Pendidikan, values["education"]), Error: errs["education"]},
		{Key: "occupation", Label: "Pekerjaan", Input: "select", Value: values["occupation"], Options: refOptions(ref.Pekerjaan, values["occupation"]), Error: errs["occupation"]},
		{Key: "income", Label: "Penghasilan bulanan", Input: "select", Value: values["income"], Options: refOptions(ref.Penghasilan, values["income"]), Error: errs["income"]},
		{Key: "special_needs", Label: "Kebutuhan khusus", Input: "multi", Options: refOptions(ref.KebutuhanKhusus, ""), Checked: checkedSet(values["special_needs"]), Error: errs["special_needs"]},
	}
}

func refOptions(l ref.List, selected string) []Option {
	opts := make([]Option, 0, len(l.Options))
	for _, o := range l.Options {
		opts = append(opts, Option{Value: o.Code, Label: o.Label, Selected: o.Code == selected})
	}
	return opts
}

func checkedSet(csv string) map[string]bool {
	m := map[string]bool{}
	for _, c := range strings.Split(csv, ",") {
		if c != "" {
			m[c] = true
		}
	}
	return m
}
