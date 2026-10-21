package builder

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestFormatCell(t *testing.T) {
	for _, tc := range []struct {
		typ  ColumnType
		v    any
		want string
	}{
		{TypeMoney, int64(150000), "Rp150.000"},
		{TypeInt, int32(12), "12"},
		{TypeInt, int16(30), "30"},
		{TypeDate, time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC), "10 September 2026"},
		{TypeText, "sakit", "sakit"},
		{TypeText, nil, "–"},
	} {
		if got := FormatCell(tc.typ, tc.v); got != tc.want {
			t.Errorf("FormatCell(%s, %v) = %q, want %q", tc.typ, tc.v, got, tc.want)
		}
	}
}

// seedAttendance menambah n sesi absensi harian untuk siswa pertama sekolah a.
func seedAttendance(t *testing.T, ctx context.Context, exec func(string, ...any), schoolID, yearID, classID, enrollmentID, teacherID string, n int) {
	t.Helper()
	exec(`WITH s AS (
		INSERT INTO attendance_sessions (school_id, academic_year_id, class_id, session_date, created_by)
		SELECT $1, $2, $3, date '2026-07-14' + g, $5 FROM generate_series(0, $6 - 1) g RETURNING id)
	INSERT INTO attendance_records (school_id, session_id, enrollment_id, status) SELECT $1, s.id, $4, 'sakit' FROM s`,
		schoolID, yearID, classID, enrollmentID, teacherID, n)
}

func TestPreview(t *testing.T) {
	tx, a, _ := seededTx(t)
	ctx := context.Background()
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := tx.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	seedAttendance(t, ctx, exec, a.ID, a.YearID, a.ClassID, a.EnrollmentIDs[0], a.Teacher.ID, 80)
	svc := NewConfigService(tx, Default)
	admin := ident(a, a.Admin)

	cfg := Config{Source: "attendance", Columns: []string{"nis", "student_name", "session_date", "status"},
		Filters: []Filter{{Column: "status", Op: OpEq, Value: "sakit"}}, Order: []Order{{Column: "session_date"}}}
	start := time.Now()
	p, err := svc.Preview(ctx, admin, cfg)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("preview of %d rows in %s", len(p.Rows), elapsed)
	if elapsed > 2*time.Second {
		t.Errorf("preview took %s", elapsed)
	}
	if len(p.Rows) != PreviewRows || !p.More || len(p.Columns) != 4 || p.Columns[2].Label != "Tanggal" {
		t.Fatalf("preview: %d rows, more %v, %d columns", len(p.Rows), p.More, len(p.Columns))
	}
	if p.Rows[0][2] != "14 Juli 2026" || p.Rows[0][3] != "sakit" || !strings.Contains(p.Rows[0][1], a.Marker) {
		t.Errorf("first row: %v", p.Rows[0])
	}
	// Uang diformat rupiah.
	pay, err := svc.Preview(ctx, admin, Config{Source: "payments", Columns: []string{"receipt_number", "amount"}, Order: []Order{{Column: "amount", Desc: true}}})
	if err != nil || len(pay.Rows) == 0 || !strings.HasPrefix(pay.Rows[0][1], "Rp") || pay.More {
		t.Errorf("payments preview: %v %v", err, pay.Rows)
	}

	// Validasi server: operator tidak cocok tipe tanggal, sumber di luar hak peran.
	for _, tc := range []struct {
		name  string
		cfg   Config
		field string
	}{
		{"contains on date", Config{Source: "attendance", Columns: []string{"nis"}, Filters: []Filter{{Column: "session_date", Op: OpContains, Value: "2026"}}}, "filters"},
		{"bad date value", Config{Source: "attendance", Columns: []string{"nis"}, Filters: []Filter{{Column: "session_date", Op: OpGte, Value: "14/07/2026"}}}, "filters"},
		{"unknown column", Config{Source: "attendance", Columns: []string{"nis", "e.id"}}, "columns"},
	} {
		_, err := svc.Preview(ctx, admin, tc.cfg)
		if fields := cfgErr(t, err); fields[tc.field] == "" {
			t.Errorf("%s: %v", tc.name, fields)
		}
	}
	if _, err := svc.Preview(ctx, ident(a, a.Treasurer), cfg); cfgErr(t, err)["source"] == "" {
		t.Error("treasurer previewed attendance")
	}
	if _, err := svc.Preview(ctx, ident(a, a.Teacher), Config{Source: "payments", Columns: []string{"nis"}}); cfgErr(t, err)["source"] == "" {
		t.Error("teacher previewed payments")
	}
	// Pratinjau tidak meninggalkan transaksi baca-saja: tulis tetap bisa.
	exec(`UPDATE schools SET name = name WHERE id = $1`, a.ID)
	var ro string
	if err := tx.QueryRow(ctx, `SHOW transaction_read_only`).Scan(&ro); err != nil || ro != "off" {
		t.Errorf("transaction_read_only after preview = %q (%v)", ro, err)
	}
}

var stateAttr = regexp.MustCompile(`data-state="([^"]*)"`)

func builderStateOf(t *testing.T, body string) builderState {
	t.Helper()
	m := stateAttr.FindStringSubmatch(body)
	if m == nil {
		t.Fatal("no data-state attribute")
	}
	var st builderState
	if err := json.Unmarshal([]byte(html.UnescapeString(m[1])), &st); err != nil {
		t.Fatalf("state JSON: %v", err)
	}
	return st
}

func TestBuilderHandler(t *testing.T) {
	tx, a, b := seededTx(t)
	do := configRouter(t, tx, a)
	ctx := context.Background()

	// Guru tidak punya sumber: halaman dan POST langsung ditolak.
	if rec := do(http.MethodGet, "/laporan/bangun", a.Teacher, nil); rec.Code != http.StatusForbidden {
		t.Errorf("teacher GET: %d", rec.Code)
	}
	if rec := do(http.MethodPost, "/laporan/bangun/pratinjau", a.Teacher, url.Values{"source": {"payments"}, "columns_payments": {"nis"}}); rec.Code != http.StatusForbidden {
		t.Errorf("teacher POST finance preview: %d", rec.Code)
	}
	// Bendahara: daftar sumber hanya keuangan; sumber lain ditolak di server.
	rec := do(http.MethodGet, "/laporan/bangun", a.Treasurer, nil)
	st := builderStateOf(t, rec.Body.String())
	var names []string
	for _, s := range st.Sources {
		names = append(names, s.Name)
	}
	if rec.Code != http.StatusOK || strings.Join(names, ",") != "arrears,payments" || !strings.Contains(rec.Body.String(), "js/report-builder.js") {
		t.Errorf("treasurer sources %v (status %d)", names, rec.Code)
	}
	if rec := do(http.MethodPost, "/laporan/bangun/pratinjau", a.Treasurer, url.Values{"source": {"attendance"}, "columns_attendance": {"nis"}}); rec.Code != http.StatusUnprocessableEntity ||
		!strings.Contains(rec.Body.String(), "tidak punya akses") {
		t.Errorf("treasurer attendance preview: %d", rec.Code)
	}
	// Kepala sekolah: semua sumber.
	if st := builderStateOf(t, do(http.MethodGet, "/laporan/bangun", a.Principal, nil).Body.String()); len(st.Sources) != 4 {
		t.Errorf("principal sources: %d", len(st.Sources))
	}

	// Pratinjau dari form (urutan kolom mengikuti isian), filter tanggal dengan operator salah 422.
	form := url.Values{"source": {"payments"}, "columns_payments": {"amount", "nis"}, "f_payments_col_0": {"amount"}, "f_payments_op_0": {">="}, "f_payments_val_0": {"1"}}
	rec = do(http.MethodPost, "/laporan/bangun/pratinjau", a.Treasurer, form)
	body := rec.Body.String()
	if rec.Code != http.StatusOK || !strings.Contains(body, "<th class=\"text-right\">Nominal</th><th>NIS</th>") || !strings.Contains(body, "Rp100.000") {
		t.Errorf("preview page: %d", rec.Code)
	}
	if st := builderStateOf(t, body); st.Source != "payments" || strings.Join(st.Columns, ",") != "amount,nis" || len(st.Filters) != 1 || st.Filters[0].Value != "1" {
		t.Errorf("state after preview: %+v", st)
	}
	bad := url.Values{"source": {"payments"}, "columns_payments": {"nis"}, "f_payments_col_0": {"payment_date"}, "f_payments_op_0": {"contains"}, "f_payments_val_0": {"2026"}}
	if rec := do(http.MethodPost, "/laporan/bangun/pratinjau", a.Treasurer, bad); rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "tidak cocok") {
		t.Errorf("date filter with contains: %d", rec.Code)
	}

	// Simpan dari pembangun, lalu buka lagi untuk diubah.
	form.Set("name", "Pembayaran besar")
	rec = do(http.MethodPost, "/laporan/bangun/simpan", a.Treasurer, form)
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusSeeOther || !strings.HasPrefix(loc, "/laporan/bangun?konfigurasi=") {
		t.Fatalf("save: %d %s", rec.Code, loc)
	}
	configID := strings.TrimPrefix(loc, "/laporan/bangun?konfigurasi=")
	if body := do(http.MethodGet, loc, a.Treasurer, nil).Body.String(); !strings.Contains(body, `name="config_id" value="`+configID+`"`) || !strings.Contains(body, `value="Pembayaran besar"`) {
		t.Error("reopen own config")
	}
	form.Set("config_id", configID)
	form.Set("shared", "on")
	if rec := do(http.MethodPost, "/laporan/bangun/simpan", a.Treasurer, form); rec.Code != http.StatusSeeOther {
		t.Errorf("update from builder: %d", rec.Code)
	}
	// Kepala sekolah membuka konfigurasi bersama: salinan (tanpa config_id).
	body = do(http.MethodGet, loc, a.Principal, nil).Body.String()
	if strings.Contains(body, `name="config_id"`) || !strings.Contains(body, "(salinan)") {
		t.Error("shared config opened by another user is not a copy")
	}
	// Mengubah milik orang lain 403; sekolah lain 404; konfigurasi pribadi orang lain 404.
	if rec := do(http.MethodPost, "/laporan/bangun/simpan", a.Principal, form); rec.Code != http.StatusForbidden {
		t.Errorf("save over someone else's config: %d", rec.Code)
	}
	form.Set("config_id", b.ReportConfigID)
	if rec := do(http.MethodPost, "/laporan/bangun/simpan", a.Admin, form); rec.Code != http.StatusNotFound {
		t.Errorf("save over other school's config: %d", rec.Code)
	}
	if rec := do(http.MethodGet, "/laporan/bangun?konfigurasi="+a.ReportConfigID, a.Principal, nil); rec.Code != http.StatusNotFound {
		t.Errorf("open someone else's private config: %d", rec.Code)
	}
	var n int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM report_configs WHERE school_id = $1 AND name = 'Pembayaran besar'`, a.ID).Scan(&n)
	if n != 1 {
		t.Errorf("configs named 'Pembayaran besar': %d", n)
	}
}
