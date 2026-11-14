package dapodik

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/academic"
	"akra/internal/audit"
	"akra/internal/auth"
	"akra/internal/classes"
	"akra/internal/grades"
	"akra/internal/school"
)

const (
	sessionGradeFile  = "dapodik_grade_file" // "<school_id>|<token>|<nama file>"
	maxGradeFile      = 5 << 20
	ActionGradeFill   = "dapodik.grade_fill"
	msgGradeForbidden = "Anda hanya bisa mengisi file untuk mapel dan kelas yang Anda ampu."
)

type gradeView struct {
	User     auth.Identity
	Classes  []option
	Subjects []option
	Info     *GradeFileInfo
	Report   *GradeFillReport
	Errors   map[string]string
	Flash    string
	Error    string
}

func gradeKey(schoolID, token string) string {
	return "schools/" + schoolID + "/dapodik-nilai/" + token + ".enc"
}

func (h *Handler) gradeOptions(r *http.Request, id auth.Identity, view *gradeView) error {
	ctx := r.Context()
	year, err := school.GetActiveAcademicYear(ctx, h.db, id.SchoolID)
	if err != nil && !errors.Is(err, school.ErrNotFound) {
		return err
	}
	if err == nil {
		list, err := classes.ListByYear(ctx, h.db, id.SchoolID, year.ID)
		if err != nil {
			return err
		}
		for _, c := range list {
			view.Classes = append(view.Classes, option{Value: c.ID, Label: c.Name})
		}
	}
	subs, err := academic.ListAllSubjects(ctx, h.db, id.SchoolID)
	if err != nil {
		return err
	}
	for _, s := range subs {
		view.Subjects = append(view.Subjects, option{Value: s.ID, Label: s.Name})
	}
	return nil
}

// gradeForm menampilkan form isi format import nilai. Peran: admin_tu,
// guru (hanya mapel yang diampu).
func (h *Handler) gradeForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	view := gradeView{User: id, Flash: auth.PopFlash(r.Context(), h.sessions)}
	if err := h.gradeOptions(r, id, &view); err != nil {
		h.serverError(w, r, "grade options", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "dapodik/grades", view)
}

// gradeFill mengisi file format import nilai dari Dapodik dengan nilai
// akhir AKRA. Peran: admin_tu, guru (hanya mapel dan kelas yang diampu).
func (h *Handler) gradeFill(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	view := gradeView{User: id}
	fail := func(status int, msg string, errs map[string]string) {
		view.Error, view.Errors = msg, errs
		if err := h.gradeOptions(r, id, &view); err != nil {
			h.serverError(w, r, "grade options", err)
			return
		}
		h.render.Render(w, r, status, "dapodik/grades", view)
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxGradeFile+64<<10)
	file, hdr, err := r.FormFile("file")
	if err != nil {
		fail(http.StatusBadRequest, "Pilih file format import nilai dari Dapodik.", nil)
		return
	}
	defer file.Close()
	data, err := readAllLimited(file, maxGradeFile)
	if err != nil {
		fail(http.StatusBadRequest, "Ukuran file maksimal 5 MB.", nil)
		return
	}
	classID, subjectID := r.FormValue("class_id"), r.FormValue("subject_id")
	class, err := classes.GetByID(ctx, h.db, id.SchoolID, classID)
	if err != nil {
		fail(http.StatusBadRequest, "", map[string]string{"class_id": "Pilih kelas."})
		return
	}
	sub, err := academic.GetSubjectByID(ctx, h.db, id.SchoolID, subjectID)
	if err != nil {
		fail(http.StatusBadRequest, "", map[string]string{"subject_id": "Pilih mapel."})
		return
	}
	if !id.HasRole(auth.RoleAdminTU) {
		ok, err := grades.CanEnterGrades(ctx, h.db, id, class.ID, sub.ID)
		if err != nil {
			h.serverError(w, r, "check grade access", err)
			return
		}
		if !ok {
			fail(http.StatusForbidden, msgGradeForbidden, nil)
			return
		}
	}
	info, _, err := ReadGradeFile(data)
	if errors.Is(err, ErrNotGradeFormat) {
		fail(http.StatusBadRequest, "File ini bukan format import nilai Dapodik. Unduh dari menu nilai rapor di Dapodik (Export dan Import → Unduh Format Excel untuk Import).", nil)
		return
	}
	if err != nil {
		h.serverError(w, r, "read grade file", err)
		return
	}
	summary, err := grades.SubjectSummary(ctx, h.db, id.SchoolID, class, sub)
	if err != nil {
		h.serverError(w, r, "subject summary", err)
		return
	}
	byNIS := map[string]grades.StudentGrades{}
	ids := make([]string, 0, len(summary.Rows))
	for _, row := range summary.Rows {
		byNIS[row.NIS] = row
		ids = append(ids, row.StudentID)
	}
	byNISN := map[string]grades.StudentGrades{}
	nrows, err := h.db.Query(ctx, `SELECT student_id::text, nisn FROM student_profiles WHERE school_id = $1 AND student_id::text = ANY ($2) AND nisn IS NOT NULL`, id.SchoolID, ids)
	if err != nil {
		h.serverError(w, r, "list nisn", err)
		return
	}
	pairs, err := pgx.CollectRows(nrows, func(row pgx.CollectableRow) ([2]string, error) {
		var p [2]string
		return p, row.Scan(&p[0], &p[1])
	})
	if err != nil {
		h.serverError(w, r, "list nisn", err)
		return
	}
	byID := map[string]grades.StudentGrades{}
	for _, row := range summary.Rows {
		byID[row.StudentID] = row
	}
	for _, p := range pairs {
		byNISN[p[1]] = byID[p[0]]
	}
	lookup := func(nisn, nis string) (GradeEntry, bool, bool) {
		row, ok := byNISN[nisn]
		if !ok || nisn == "" {
			row, ok = byNIS[nis]
		}
		if !ok {
			return GradeEntry{}, false, false
		}
		if row.Result.Incomplete || !summary.Configured {
			return GradeEntry{}, true, false
		}
		return GradeEntry{Score: row.Result.Score, Letter: row.Letter}, true, true
	}
	out, rep, err := FillGradeFile(data, summary.KKM, lookup)
	if err != nil {
		h.serverError(w, r, "fill grade file", err)
		return
	}
	var tb [16]byte
	if _, err := rand.Read(tb[:]); err != nil {
		h.serverError(w, r, "token", err)
		return
	}
	token := hex.EncodeToString(tb[:])
	ct, err := h.keys.Encrypt(string(out), "dapodik-nilai:"+token)
	if err != nil {
		h.serverError(w, r, "encrypt grade file", err)
		return
	}
	if err := h.store.Put(ctx, gradeKey(id.SchoolID, token), bytes.NewReader(ct), int64(len(ct)), "application/octet-stream"); err != nil {
		h.serverError(w, r, "store grade file", err)
		return
	}
	name := strings.NewReplacer("\"", "", "|", "", "/", "-").Replace(hdr.Filename)
	if !strings.HasSuffix(strings.ToLower(name), ".xlsx") {
		name += ".xlsx"
	}
	h.sessions.Put(ctx, sessionGradeFile, id.SchoolID+"|"+token+"|terisi-"+name)
	if err := pgx.BeginFunc(ctx, h.db, func(tx pgx.Tx) error {
		return audit.Record(ctx, tx, audit.Entry{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r),
			Action: ActionGradeFill, EntityType: "class", EntityID: class.ID,
			After: map[string]any{"subject_id": sub.ID, "filled": rep.Filled, "unmatched": len(rep.Unmatched), "no_score": len(rep.NoScore)}})
	}); err != nil {
		h.serverError(w, r, "audit grade fill", err)
		return
	}
	view.Info, view.Report = &info, &rep
	if !strings.EqualFold(strings.TrimSpace(info.Subject), sub.Name) || (info.ClassName != "" && !strings.EqualFold(strings.TrimSpace(info.ClassName), class.Name)) {
		view.Error = "Perhatian: mapel/rombel di file (" + info.Subject + ", " + info.ClassName + ") berbeda dengan pilihan (" + sub.Name + ", " + class.Name + "). Pastikan file yang benar."
	}
	if err := h.gradeOptions(r, id, &view); err != nil {
		h.serverError(w, r, "grade options", err)
		return
	}
	h.render.Render(w, r, http.StatusOK, "dapodik/grades", view)
}

// gradeDownload mengunduh file hasil pengisian terakhir di sesi. Peran:
// admin_tu, guru.
func (h *Handler) gradeDownload(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	parts := strings.SplitN(h.sessions.GetString(r.Context(), sessionGradeFile), "|", 3)
	if len(parts) != 3 || parts[0] != id.SchoolID {
		http.Redirect(w, r, basePath+"/nilai", http.StatusSeeOther)
		return
	}
	body, err := h.store.Get(r.Context(), gradeKey(id.SchoolID, parts[1]))
	if err != nil {
		http.Redirect(w, r, basePath+"/nilai", http.StatusSeeOther)
		return
	}
	ct, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		h.serverError(w, r, "read grade file", err)
		return
	}
	data, err := h.keys.Decrypt(ct, "dapodik-nilai:"+parts[1])
	if err != nil {
		h.serverError(w, r, "decrypt grade file", err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	w.Header().Set("Content-Disposition", `attachment; filename="`+parts[2]+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	_, _ = w.Write([]byte(data))
}
