package dapodik

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
	"akra/internal/dapodik/ref"
	"akra/internal/importer"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/school"
	"akra/internal/students"
	"akra/internal/wilayah"
)

// ActionImportPD dicatat setiap impor ekspor Daftar PD diterapkan (T101).
const ActionImportPD = "dapodik.import_pd"

// pdField memetakan satu kolom ekspor Daftar PD ke field data rinci AKRA.
type pdField struct {
	Header string
	Key    string    // key students.ProfileFields
	List   *ref.List // kolom berisi label pilihan Dapodik
	Bool   bool      // kolom "Ya"/"Tidak"
}

var pdProfileFields = []pdField{
	{Header: "NISN", Key: "nisn"}, {Header: "Tempat Lahir", Key: "birth_place"}, {Header: "NIK", Key: "nik"},
	{Header: "Agama", Key: "religion", List: &ref.Agama}, {Header: "Alamat", Key: "street"}, {Header: "RT", Key: "rt"},
	{Header: "RW", Key: "rw"}, {Header: "Dusun", Key: "hamlet"}, {Header: "Kelurahan", Key: "village"}, {Header: "Kecamatan", Key: "district"},
	{Header: "Kode Pos", Key: "postal_code"}, {Header: "Jenis Tinggal", Key: "residence_type", List: &ref.TempatTinggal},
	{Header: "Alat Transportasi", Key: "transport_mode", List: &ref.ModaTransportasi}, {Header: "Telepon", Key: "home_phone"},
	{Header: "HP", Key: "mobile_phone"}, {Header: "E-Mail", Key: "email"}, {Header: "SKHUN", Key: "skhun_number"},
	{Header: "No Peserta Ujian Nasional", Key: "exam_number"}, {Header: "No Seri Ijazah", Key: "diploma_serial"},
	{Header: "Penerima KIP", Key: "has_kip", Bool: true}, {Header: "Nomor KIP", Key: "kip_number"}, {Header: "Nama di KIP", Key: "kip_name"},
	{Header: "Nomor KKS", Key: "kks_number"}, {Header: "No Registrasi Akta Lahir", Key: "birth_cert_number"},
	{Header: "Layak PIP (usulan dari sekolah)", Key: "pip_eligible", Bool: true}, {Header: "Alasan Layak PIP", Key: "pip_reason", List: &ref.AlasanPIP},
	{Header: "Kebutuhan Khusus", Key: "special_needs", List: &ref.KebutuhanKhusus}, {Header: "Sekolah Asal", Key: "previous_school"},
	{Header: "Anak ke-berapa", Key: "child_order"}, {Header: "Lintang", Key: "latitude"}, {Header: "Bujur", Key: "longitude"},
	{Header: "No KK", Key: "kk_number"}, {Header: "Berat Badan", Key: "weight_kg"}, {Header: "Tinggi Badan", Key: "height_cm"},
	{Header: "Lingkar Kepala", Key: "head_cm"}, {Header: "Jml. Saudara\nKandung", Key: "siblings"},
	{Header: "Jarak Rumah\nke Sekolah (KM)", Key: "distance_km"},
}

var pdParentGroups = map[string]string{"Data Ayah": students.RelationFather, "Data Ibu": students.RelationMother, "Data Wali": students.RelationGuardian}

// Change adalah satu perubahan field.
type Change struct {
	Key, Label string
	Current    string // nilai di AKRA (tampilan)
	Incoming   string // nilai dari file (tampilan)
}

// PlanRow adalah rencana untuk satu baris file.
type PlanRow struct {
	Line      int
	NIS       string
	Name      string
	StudentID string // kosong = siswa baru
	MatchBy   string // "NIS", "NISN", "NIK", atau kosong
	ClassName string
	Fills     []Change // field kosong di AKRA yang akan diisi
	Conflicts []Change // field berbeda; diganti hanya jika dipilih
	Errors    []string
	values    map[string]string // nilai kanonik dari file (key ProfileFields)
	parents   map[string]map[string]string
	gender    string
	birth     *time.Time
}

// Plan adalah hasil pemeriksaan file ekspor sebelum diterapkan.
type Plan struct {
	Rows      []PlanRow
	New       int
	Matched   int
	WithError int
}

func normHeader(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }

// parseDate menerima yyyy-mm-dd, dd/mm/yyyy, dd-mm-yyyy, atau nomor seri
// tanggal Excel.
func parseDate(v string) (*time.Time, bool) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, true
	}
	for _, layout := range []string{"2006-01-02", "02/01/2006", "02-01-2006", "2/1/2006"} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t, true
		}
	}
	if n, err := strconv.ParseFloat(v, 64); err == nil && n > 1 && n < 80000 {
		t := time.Date(1899, 12, 30, 0, 0, 0, 0, time.UTC).AddDate(0, 0, int(n))
		return &t, true
	}
	return nil, false
}

// PlanPD membaca file ekspor Daftar PD Dapodik dan menyusun rencana impor.
// Tidak menulis ke database.
func PlanPD(ctx context.Context, db database.DB, keys *pii.Keyring, schoolID string, r io.Reader) (Plan, error) {
	t, err := importer.ReadTable(r)
	if err != nil {
		return Plan{}, err
	}
	col := map[string]int{}
	for i, h := range t.Header {
		col[normHeader(h)] = i
	}
	if _, ok := col["nipd"]; !ok {
		return Plan{}, ErrNotPDExport
	}
	if _, ok := col["nama"]; !ok {
		return Plan{}, ErrNotPDExport
	}
	var plan Plan
	for i, cells := range t.Rows {
		get := func(h string) string {
			if c, ok := col[normHeader(h)]; ok && c < len(cells) {
				return strings.TrimSpace(cells[c])
			}
			return ""
		}
		row := PlanRow{Line: t.Lines[i], NIS: get("NIPD"), Name: strings.Join(strings.Fields(get("Nama")), " "), ClassName: get("Rombel Saat Ini"),
			values: map[string]string{}, parents: map[string]map[string]string{}}
		if row.Name == "" && row.NIS == "" {
			continue
		}
		switch jk := strings.ToUpper(get("JK")); jk {
		case "L", "P":
			row.gender = jk
		case "":
		default:
			row.Errors = append(row.Errors, "JK harus L atau P")
		}
		if d, ok := parseDate(get("Tanggal Lahir")); ok {
			row.birth = d
		} else {
			row.Errors = append(row.Errors, "Tanggal lahir tidak dikenali")
		}
		for _, f := range pdProfileFields {
			v := get(f.Header)
			if v == "" {
				continue
			}
			switch {
			case f.Bool:
				row.values[f.Key] = strconv.FormatBool(strings.EqualFold(v, "ya"))
			case f.List != nil:
				code, ok := labelsToCodes(*f.List, v)
				if !ok {
					row.Errors = append(row.Errors, fmt.Sprintf("%s %q tidak dikenal", strings.ReplaceAll(f.Header, "\n", " "), v))
					continue
				}
				row.values[f.Key] = code
			default:
				row.values[f.Key] = v
			}
		}
		for group, role := range pdParentGroups {
			p := map[string]string{"full_name": get(group + " / Nama"), "birth_year": get(group + " / Tahun Lahir"), "nik": get(group + " / NIK")}
			for _, x := range []struct {
				sub, key string
				list     ref.List
			}{{"Jenjang Pendidikan", "education", ref.Pendidikan}, {"Pekerjaan", "occupation", ref.Pekerjaan}, {"Penghasilan", "income", ref.Penghasilan}} {
				v := get(group + " / " + x.sub)
				if v == "" {
					continue
				}
				if code, ok := labelsToCodes(x.list, v); ok {
					p[x.key] = code
				} else {
					row.Errors = append(row.Errors, fmt.Sprintf("%s %s %q tidak dikenal", group, x.sub, v))
				}
			}
			if p["full_name"] != "" {
				row.parents[role] = p
			}
		}
		if row.NIS == "" {
			row.Errors = append(row.Errors, "NIPD kosong")
		}
		// Kode desa (T110): dicocokkan dari nama desa + kecamatan bila unik;
		// jika tidak, alamat tetap tersimpan sebagai isian manual.
		if row.values["wilayah_kode"] == "" {
			kode, ok, err := wilayah.Match(ctx, db, row.values["district"], row.values["village"])
			if err != nil {
				return Plan{}, err
			}
			if ok {
				row.values["wilayah_kode"] = kode
			}
		}
		if err := matchStudent(ctx, db, keys, schoolID, &row); err != nil {
			return Plan{}, err
		}
		if row.StudentID != "" {
			if err := diffStudent(ctx, db, keys, schoolID, &row); err != nil {
				return Plan{}, err
			}
			plan.Matched++
		} else if len(row.Errors) == 0 {
			plan.New++
			for _, f := range students.ProfileFields {
				if v := row.values[f.Key]; v != "" {
					row.Fills = append(row.Fills, Change{Key: f.Key, Label: f.Label, Incoming: display(f, v)})
				}
			}
		}
		if len(row.Errors) > 0 {
			plan.WithError++
		}
		plan.Rows = append(plan.Rows, row)
	}
	return plan, nil
}

// ErrNotPDExport dikembalikan jika file tidak punya kolom NIPD dan Nama.
var ErrNotPDExport = errors.New("dapodik: file is not a Daftar Peserta Didik export")

// labelsToCodes mengubah label (atau beberapa label dipisah koma) ke kode.
func labelsToCodes(l ref.List, v string) (string, bool) {
	if c, err := l.Code(v); err == nil {
		return c, true
	}
	var codes []string
	for _, part := range splitLabels(l, v) {
		c, err := l.Code(part)
		if err != nil {
			return "", false
		}
		codes = append(codes, c)
	}
	return strings.Join(codes, ","), len(codes) > 0
}

// splitLabels memecah "Netra (A), Rungu (B)" tanpa memecah label yang
// memuat koma sendiri.
func splitLabels(l ref.List, v string) []string {
	var out []string
	rest := strings.TrimSpace(v)
	for rest != "" {
		found := false
		for _, o := range l.Options {
			if strings.HasPrefix(rest, o.Label) {
				out = append(out, o.Label)
				rest = strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(rest, o.Label), ","))
				found = true
				break
			}
		}
		if !found {
			return append(out, rest)
		}
	}
	return out
}

func display(f students.ProfileField, v string) string {
	if f.Ref != nil {
		return labels(*f.Ref, v)
	}
	if v == "true" {
		return "Ya"
	}
	if v == "false" {
		return "Tidak"
	}
	return v
}

// matchStudent mencari siswa lewat NIS (NIPD), lalu NISN, lalu NIK.
func matchStudent(ctx context.Context, db database.DBTX, keys *pii.Keyring, schoolID string, row *PlanRow) error {
	try := func(by, sql string, arg any) error {
		if row.StudentID != "" || arg == nil {
			return nil
		}
		err := db.QueryRow(ctx, sql, schoolID, arg).Scan(&row.StudentID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err == nil {
			row.MatchBy = by
		}
		return err
	}
	var nis, nisn, nikIdx any
	if row.NIS != "" {
		nis = row.NIS
	}
	if v := row.values["nisn"]; v != "" {
		nisn = v
	}
	if v := row.values["nik"]; v != "" {
		nikIdx = keys.BlindIndex("nik", v)
	}
	if err := try("NIS", `SELECT id FROM students WHERE school_id = $1 AND nis = $2`, nis); err != nil {
		return err
	}
	if err := try("NISN", `SELECT student_id FROM student_profiles WHERE school_id = $1 AND nisn = $2`, nisn); err != nil {
		return err
	}
	return try("NIK", `SELECT student_id FROM student_profiles WHERE school_id = $1 AND nik_idx = $2`, nikIdx)
}

// diffStudent membandingkan nilai file dengan data rinci siswa.
func diffStudent(ctx context.Context, db database.DB, keys *pii.Keyring, schoolID string, row *PlanRow) error {
	prof, err := students.NewProfileService(db, keys).Get(ctx, schoolID, row.StudentID)
	if err != nil {
		return err
	}
	for _, f := range students.ProfileFields {
		in := row.values[f.Key]
		if in == "" {
			continue
		}
		cur := prof.Secret(f.Key).Reveal()
		if (f.Key == "has_kip" || f.Key == "pip_eligible") && cur == "false" {
			cur = "" // ya/tidak yang belum pernah diisi tersimpan "false"
		}
		switch {
		case cur == "" && in == "false":
			// "Tidak" untuk field ya/tidak yang belum diisi: tidak ada perubahan.
		case cur == "":
			row.Fills = append(row.Fills, Change{Key: f.Key, Label: f.Label, Incoming: display(f, in)})
		case cur != in:
			row.Conflicts = append(row.Conflicts, Change{Key: f.Key, Label: f.Label, Current: display(f, cur), Incoming: display(f, in)})
		}
	}
	return nil
}

// ApplyResult adalah ringkasan penerapan impor.
type ApplyResult struct {
	Created, Updated int
	Failed           []string // "baris N: pesan"
}

// ApplyPD menerapkan rencana: field kosong diisi, konflik diganti hanya
// jika key "<baris>:<field>" ada di overwrite, siswa baru dibuat (dan
// ditempatkan di rombel dengan nama sama di tahun ajaran aktif jika ada).
// Orang tua dibuat jika belum ada; data rincinya hanya mengisi yang kosong.
// Setiap baris diproses di savepoint sendiri; baris yang gagal dilewati.
func ApplyPD(ctx context.Context, db database.DB, keys *pii.Keyring, actor school.Actor, plan Plan, overwrite map[string]bool) (ApplyResult, error) {
	var res ApplyResult
	err := pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		for _, row := range plan.Rows {
			if len(row.Errors) > 0 {
				res.Failed = append(res.Failed, fmt.Sprintf("baris %d: %s", row.Line, strings.Join(row.Errors, "; ")))
				continue
			}
			created := row.StudentID == ""
			err := pgx.BeginFunc(ctx, tx, func(sp pgx.Tx) error { return applyRow(ctx, sp, keys, actor, &row, overwrite) })
			if err != nil {
				res.Failed = append(res.Failed, fmt.Sprintf("baris %d: %s", row.Line, applyErrorText(err)))
				continue
			}
			if created {
				res.Created++
			} else {
				res.Updated++
			}
		}
		return audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: ActionImportPD, EntityType: "school", EntityID: actor.SchoolID,
			After: map[string]any{"created": res.Created, "updated": res.Updated, "failed": len(res.Failed)}})
	})
	return res, err
}

func applyErrorText(err error) string {
	var ve *students.ValidationError
	if errors.As(err, &ve) {
		var parts []string
		for _, m := range ve.Fields {
			parts = append(parts, m)
		}
		return strings.Join(parts, " ")
	}
	if errors.Is(err, students.ErrDuplicateNIS) {
		return "NIS sudah dipakai siswa lain."
	}
	return "gagal disimpan"
}

func applyRow(ctx context.Context, tx pgx.Tx, keys *pii.Keyring, actor school.Actor, row *PlanRow, overwrite map[string]bool) error {
	if row.StudentID == "" {
		st, err := students.Create(ctx, tx, actor.SchoolID, students.StudentInput{NIS: row.NIS, FullName: row.Name, BirthDate: row.birth, Gender: row.gender})
		if err != nil {
			return err
		}
		row.StudentID = st.ID
		if err := audit.Record(ctx, tx, audit.Entry{SchoolID: actor.SchoolID, UserID: actor.UserID, IP: actor.IP,
			Action: students.ActionStudentCreate, EntityType: "student", EntityID: st.ID, After: map[string]any{"nis": row.NIS, "source": "dapodik"}}); err != nil {
			return err
		}
		if row.ClassName != "" {
			var classID, yearID string
			var start time.Time
			err := tx.QueryRow(ctx, `SELECT c.id, y.id, greatest(y.start_date, current_date) FROM classes c JOIN academic_years y ON y.id = c.academic_year_id
				WHERE c.school_id = $1 AND y.is_active AND lower(c.name) = lower($2)`, actor.SchoolID, row.ClassName).Scan(&classID, &yearID, &start)
			if err == nil {
				if _, err := students.Place(ctx, tx, actor.SchoolID, st.ID, classID, yearID, start); err != nil {
					return err
				}
			} else if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		}
	}
	values := map[string]string{}
	for _, c := range row.Fills {
		values[c.Key] = row.values[c.Key]
	}
	for _, c := range row.Conflicts {
		if overwrite[strconv.Itoa(row.Line)+":"+c.Key] {
			values[c.Key] = row.values[c.Key]
		}
	}
	if len(values) > 0 {
		if _, err := students.NewProfileService(tx, keys).Save(ctx, actor, row.StudentID, values); err != nil {
			return err
		}
	}
	list, err := students.ListByStudent(ctx, tx, actor.SchoolID, row.StudentID)
	if err != nil {
		return err
	}
	details := students.NewGuardianDetailService(tx, keys)
	for role, p := range row.parents {
		form := students.GuardianDetailsForm{NIK: p["nik"], BirthYear: p["birth_year"], Education: p["education"], Occupation: p["occupation"], Income: p["income"]}
		if g, ok := students.FindParent(list, role); ok {
			cur, err := details.Get(ctx, actor.SchoolID, g.ID)
			if err != nil {
				return err
			}
			// Hanya mengisi yang kosong; data orang tua yang sudah ada tidak ditimpa.
			if cur.NIK.Reveal() != "" {
				form.NIK = cur.NIK.Reveal()
			}
			if cur.BirthYear > 0 {
				form.BirthYear = strconv.Itoa(cur.BirthYear)
			}
			for _, x := range []struct{ cur, dst *string }{{&cur.Education, &form.Education}, {&cur.Occupation, &form.Occupation}, {&cur.Income, &form.Income}} {
				if *x.cur != "" {
					*x.dst = *x.cur
				}
			}
			form.SpecialNeeds = cur.SpecialNeeds
			if _, err := details.Save(ctx, actor, g.ID, form); err != nil {
				return err
			}
			continue
		}
		if _, err := details.SaveParent(ctx, actor, row.StudentID, role, p["full_name"], form); err != nil {
			return err
		}
	}
	return nil
}
