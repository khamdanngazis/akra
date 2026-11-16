package dapodik

import (
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/auth"
)

// checkIssue adalah satu masalah beserta jenis dan tautan perbaikannya.
type checkIssue struct {
	Issue
	Kind   string
	Error  bool
	FixURL string
}

type checkStudent struct {
	ID, NIS, FullName, ClassName string
	Issues                       []checkIssue
	Errors                       int
}

// checkKind adalah satu jenis masalah dengan jumlah siswa yang terkena.
type checkKind struct {
	Key, Label, URL string
	N               int
	Active          bool
}

// checkCounts adalah ringkasan hasil cek untuk kelas dan pencarian terpilih.
type checkCounts struct {
	Checked, WithError, WarnOnly, Clean int
}

type checkView struct {
	User    auth.Identity
	CanEdit bool
	Classes []option
	ClassID string
	Level   string
	Kind    string
	Query   string
	Counts  checkCounts
	// URL kartu ringkasan (filter tingkat) dan tombol reset.
	AllURL, ErrorURL, WarnURL, ResetURL string
	Kinds                               []checkKind
	Rows                                []checkStudent
	Page, Pages, Total                  int
	PrevURL, NextURL                    string
	Flash                               string
	Error                               string
}

// checkPageSize adalah jumlah siswa bermasalah per halaman cek (T115).
const checkPageSize = 50

// checkLevels adalah filter tingkat: "error" = ada yang pasti ditolak,
// "peringatan" = hanya peringatan.
var checkLevels = map[string]bool{"error": true, "peringatan": true}

// noClass adalah nilai filter kelas untuk siswa yang belum punya kelas.
const noClass = "tanpa"

// issueKind mengelompokkan field masalah menjadi jenis yang dipahami TU,
// beserta bagian halaman data rinci tempat memperbaikinya ("" = halaman
// ubah data siswa).
func issueKind(field string) (key, label, anchor string) {
	if role, ok := strings.CutSuffix(field, ".birth_year"); ok {
		return "ortu_lahir", "Tahun lahir orang tua", role
	}
	switch field {
	case "nik":
		return "nik", "NIK", "identitas"
	case "kk_number":
		return "kk", "No KK", "identitas"
	case "nisn":
		return "nisn", "NISN", "identitas"
	case "full_name":
		return "nama", "Nama siswa", ""
	case "birth_date":
		return "lahir", "Tanggal lahir siswa", ""
	case "mother_name":
		return "ibu", "Nama ibu kandung", "ibu"
	case "father_name":
		return "ayah", "Nama ayah", "ayah"
	case "rt", "postal_code":
		return "alamat", "RT atau kode pos", "alamat"
	case "latitude", "longitude":
		return "lokasi", "Titik lokasi rumah", "alamat"
	}
	return field, field, "identitas"
}

// checkPage memeriksa semua siswa aktif dan menampilkan yang bermasalah
// dalam satu daftar, dengan ringkasan, filter tingkat, jenis masalah,
// kelas, pencarian, dan halaman (T115). Peran: admin_tu, kepala_sekolah.
func (h *Handler) checkPage(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	ctx := r.Context()
	q := r.URL.Query()
	rows, err := h.db.Query(ctx, `
		SELECT s.id, coalesce(c.id::text, ''), coalesce(c.name, ''), coalesce(c.grade_level, 0)
		FROM students s
		LEFT JOIN academic_years y ON y.school_id = s.school_id AND y.is_active
		LEFT JOIN enrollments e ON e.student_id = s.id AND e.academic_year_id = y.id AND e.end_date IS NULL
		LEFT JOIN classes c ON c.id = e.class_id
		WHERE s.school_id = $1 AND s.status = 'aktif'
		ORDER BY c.name NULLS LAST, lower(s.full_name) LIMIT 3000`, id.SchoolID)
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}
	type pick struct {
		id, classID, class string
		grade              int
	}
	picks, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (pick, error) {
		var p pick
		var g int16
		err := row.Scan(&p.id, &p.classID, &p.class, &g)
		p.grade = int(g)
		return p, err
	})
	if err != nil {
		h.serverError(w, r, "list students", err)
		return
	}

	view := checkView{User: id, CanEdit: id.HasRole(auth.RoleAdminTU), Level: q.Get("tingkat"), Kind: q.Get("masalah"),
		Query: strings.TrimSpace(q.Get("q")), Flash: auth.PopFlash(ctx, h.sessions)}
	if !checkLevels[view.Level] {
		view.Level = ""
	}
	// Pilihan kelas dari siswa yang diperiksa (urut nama kelas).
	seen := map[string]bool{}
	for _, p := range picks {
		v, label := p.classID, p.class
		if v == "" {
			v, label = noClass, "Belum punya kelas"
		}
		if !seen[v] {
			seen[v] = true
			view.Classes = append(view.Classes, option{Value: v, Label: label, Selected: v == q.Get("kelas")})
		}
	}
	if seen[q.Get("kelas")] {
		view.ClassID = q.Get("kelas")
	}

	today := id.Today()
	needle := strings.ToLower(view.Query)
	var scoped []checkStudent // bermasalah, dalam kelas dan pencarian terpilih
	for _, p := range picks {
		cls := p.classID
		if cls == "" {
			cls = noClass
		}
		if view.ClassID != "" && cls != view.ClassID {
			continue
		}
		in, err := load(ctx, h.db, h.keys, id.SchoolID, p.id)
		if err != nil {
			h.serverError(w, r, "load student", err)
			return
		}
		if needle != "" && !strings.Contains(strings.ToLower(in.Student.FullName), needle) && !strings.HasPrefix(in.Student.NIS, view.Query) {
			continue
		}
		view.Counts.Checked++
		issues := Check(in, p.grade, today)
		if len(issues) == 0 {
			continue
		}
		cs := checkStudent{ID: in.Student.ID, NIS: in.Student.NIS, FullName: in.Student.FullName, ClassName: p.class}
		for _, is := range issues {
			key, _, anchor := issueKind(is.Field)
			ci := checkIssue{Issue: is, Kind: key, Error: is.Level == LevelError, FixURL: "/siswa/" + cs.ID + "/ubah"}
			if anchor != "" {
				ci.FixURL = "/siswa/" + cs.ID + "/data-rinci#" + anchor
			}
			if ci.Error {
				cs.Errors++
			}
			cs.Issues = append(cs.Issues, ci)
		}
		// Yang pasti ditolak lebih dulu.
		sort.SliceStable(cs.Issues, func(i, j int) bool { return cs.Issues[i].Error && !cs.Issues[j].Error })
		// Satu tautan Perbaiki per bagian tujuan.
		fix := map[string]bool{}
		for i := range cs.Issues {
			if fix[cs.Issues[i].FixURL] {
				cs.Issues[i].FixURL = ""
			}
			fix[cs.Issues[i].FixURL] = true
		}
		if cs.Errors > 0 {
			view.Counts.WithError++
		} else {
			view.Counts.WarnOnly++
		}
		scoped = append(scoped, cs)
	}
	view.Counts.Clean = view.Counts.Checked - view.Counts.WithError - view.Counts.WarnOnly

	// Jenis masalah dihitung setelah filter tingkat; daftar setelah filter jenis.
	kindN := map[string]int{}
	kindLabel := map[string]string{}
	var order []string
	var leveled []checkStudent
	for _, cs := range scoped {
		if (view.Level == "error" && cs.Errors == 0) || (view.Level == "peringatan" && cs.Errors > 0) {
			continue
		}
		leveled = append(leveled, cs)
		got := map[string]bool{}
		for _, is := range cs.Issues {
			if got[is.Kind] {
				continue
			}
			got[is.Kind] = true
			if _, ok := kindN[is.Kind]; !ok {
				_, kindLabel[is.Kind], _ = issueKind(is.Field)
				order = append(order, is.Kind)
			}
			kindN[is.Kind]++
		}
	}
	if kindN[view.Kind] == 0 {
		view.Kind = ""
	}
	sort.SliceStable(order, func(i, j int) bool { return kindN[order[i]] > kindN[order[j]] })

	link := func(level, kind string, page int) string {
		v := url.Values{}
		if view.ClassID != "" {
			v.Set("kelas", view.ClassID)
		}
		if view.Query != "" {
			v.Set("q", view.Query)
		}
		if level != "" {
			v.Set("tingkat", level)
		}
		if kind != "" {
			v.Set("masalah", kind)
		}
		if page > 1 {
			v.Set("hal", strconv.Itoa(page))
		}
		if len(v) == 0 {
			return "/dapodik/cek"
		}
		return "/dapodik/cek?" + v.Encode()
	}
	view.AllURL, view.ErrorURL, view.WarnURL = link("", "", 1), link("error", "", 1), link("peringatan", "", 1)
	view.ResetURL = "/dapodik/cek"
	for _, k := range order {
		ck := checkKind{Key: k, Label: kindLabel[k], N: kindN[k], Active: k == view.Kind, URL: link(view.Level, k, 1)}
		if ck.Active {
			ck.URL = link(view.Level, "", 1) // klik lagi untuk melepas filter
		}
		view.Kinds = append(view.Kinds, ck)
	}

	var list []checkStudent
	for _, cs := range leveled {
		if view.Kind == "" || hasKind(cs, view.Kind) {
			list = append(list, cs)
		}
	}
	// Siswa dengan masalah yang pasti ditolak lebih dulu; sisanya urut kelas dan nama.
	sort.SliceStable(list, func(i, j int) bool { return list[i].Errors > 0 && list[j].Errors == 0 })

	view.Total = len(list)
	view.Pages = (view.Total + checkPageSize - 1) / checkPageSize
	view.Page, _ = strconv.Atoi(q.Get("hal"))
	if view.Page < 1 {
		view.Page = 1
	}
	if view.Pages > 0 && view.Page > view.Pages {
		view.Page = view.Pages
	}
	if view.Total > 0 {
		from := (view.Page - 1) * checkPageSize
		view.Rows = list[from:min(from+checkPageSize, view.Total)]
	}
	if view.Page > 1 {
		view.PrevURL = link(view.Level, view.Kind, view.Page-1)
	}
	if view.Page < view.Pages {
		view.NextURL = link(view.Level, view.Kind, view.Page+1)
	}
	h.render.Render(w, r, http.StatusOK, "dapodik/check", view)
}

func hasKind(cs checkStudent, kind string) bool {
	for _, is := range cs.Issues {
		if is.Kind == kind {
			return true
		}
	}
	return false
}
