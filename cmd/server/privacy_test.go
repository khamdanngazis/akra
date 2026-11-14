package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2/memstore"

	"akra/internal/auth"
	"akra/internal/dapodik"
	"akra/internal/platform/clock"
	"akra/internal/platform/database"
	"akra/internal/platform/pii"
	"akra/internal/platform/render"
	"akra/internal/platform/storage"
	"akra/internal/students"
	"akra/internal/testutil"
)

// Nomor identitas contoh; tidak boleh muncul sebagai teks di database, log,
// atau file storage (T104).
var privacySamples = map[string]string{
	"nik siswa (form)":     "3201019901010011",
	"kk siswa (form)":      "3201019901010099",
	"akta siswa (form)":    "3273-LT-99887766",
	"nik ibu (form)":       "3201019901010022",
	"nik pendaftar":        "3201019901010033",
	"nik ibu pendaftar":    "3201019901010044",
	"nik siswa (impor PD)": "3201019901010055",
}

func TestPrivacy_NoPlaintextIdentityNumbers(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	hasher := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1})
	hash, _ := hasher.HashPassword(testutil.Password)
	a, _ := testutil.SeedTwoSchools(t, tx, hash)
	rd, err := render.New(mustSub(t, "templates"), mustSub(t, "static"), logger)
	if err != nil {
		t.Fatal(err)
	}
	storeDir := t.TempDir()
	store, err := storage.NewLocal(storeDir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := newApp(appDeps{
		db: tx, pinger: pool, sessions: auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger),
		render: rd, static: mustSub(t, "static"), mailer: &captureMailer{}, hasher: hasher,
		baseURL: "https://akra.test", logger: logger, store: store, pii: pii.DevKeyring(),
	})
	if err != nil {
		t.Fatal(err)
	}
	s := privacySamples
	admin := newClient(t, app)
	if rec := admin.login(a.Code, a.Admin.Email, testutil.Password); rec.Code != http.StatusSeeOther {
		t.Fatalf("login: %d", rec.Code)
	}
	student := a.StudentIDs[0]
	admin.get("/siswa/" + student + "/data-rinci")
	expect := func(rec *httptest.ResponseRecorder, code int, what string) {
		t.Helper()
		if rec.Code != code {
			t.Fatalf("%s: status %d, want %d\n%s", what, rec.Code, code, rec.Body.String())
		}
	}
	expect(admin.do(http.MethodPost, "/siswa/"+student+"/data-rinci", url.Values{"section": {"identitas"}, "nik": {s["nik siswa (form)"]},
		"kk_number": {s["kk siswa (form)"]}, "birth_cert_number": {s["akta siswa (form)"]}, "religion": {"01"}}), http.StatusSeeOther, "save identity")
	// Error validasi: nilai salah tidak boleh tercatat di log.
	expect(admin.do(http.MethodPost, "/siswa/"+student+"/data-rinci", url.Values{"section": {"identitas"}, "nik": {s["nik siswa (form)"]}, "kk_number": {s["nik siswa (form)"]}}), http.StatusBadRequest, "nik equals kk")
	expect(admin.do(http.MethodPost, "/siswa/"+student+"/data-rinci", url.Values{"section": {"ibu"}, "full_name": {"Ibu Privasi"}, "nik": {s["nik ibu (form)"]}}), http.StatusSeeOther, "save mother")

	// Pendaftaran publik dengan dokumen.
	today := clock.Today()
	admin.get("/pendaftaran/periode/baru")
	rec := admin.do(http.MethodPost, "/pendaftaran/periode", url.Values{"name": {"Privasi"}, "academic_year_id": {a.YearID}, "target_grade": {"1"},
		"opens_on": {today.AddDate(0, 0, -1).Format("2006-01-02")}, "closes_on": {today.AddDate(0, 1, 0).Format("2006-01-02")}, "required_docs": {"kk"}})
	expect(rec, http.StatusSeeOther, "create period")
	periodPath := rec.Header().Get("Location")
	admin.get(periodPath)
	expect(admin.do(http.MethodPost, periodPath+"/jalur", url.Values{"track_name": {"Reguler"}}), http.StatusSeeOther, "add track")
	expect(admin.do(http.MethodPost, periodPath+"/status", url.Values{"status": {"buka"}}), http.StatusSeeOther, "open period")
	var periodID, trackID string
	if err := tx.QueryRow(ctx, `SELECT p.id, t.id FROM admission_periods p JOIN admission_tracks t ON t.period_id = p.id WHERE p.school_id = $1 AND p.name = 'Privasi'`, a.ID).Scan(&periodID, &trackID); err != nil {
		t.Fatal(err)
	}
	parent := newClient(t, app)
	base := "/s/" + a.Code + "/daftar"
	parent.get(base + "/")
	expect(parent.do(http.MethodPost, base+"/", url.Values{"choice": {periodID + "|" + trackID}, "full_name": {"Calon Privasi"}, "gender": {"L"},
		"birth_date": {"2019-05-17"}, "phone": {"081211119999"}, "consent": {"on"}}), http.StatusSeeOther, "draft")
	parent.get(base + "/isi/calon")
	for _, step := range []struct {
		path string
		form url.Values
	}{
		{"calon", url.Values{"full_name": {"Calon Privasi"}, "gender": {"L"}, "birth_date": {"2019-05-17"}, "nik": {s["nik pendaftar"]}, "birth_place": {"Bandung"}, "religion": {"01"}}},
		{"alamat", url.Values{"street": {"Jl. Privasi"}, "village": {"Desa"}, "district": {"Kec"}, "residence_type": {"01"}, "transport_mode": {"01"}}},
		{"orangtua", url.Values{"ibu.full_name": {"Ibu Pendaftar"}, "ibu.nik": {s["nik ibu pendaftar"]}}},
		{"sekolah", url.Values{}},
	} {
		parent.get(base + "/isi/" + step.path)
		expect(parent.do(http.MethodPost, base+"/isi/"+step.path, step.form), http.StatusSeeOther, "step "+step.path)
	}
	parent.get(base + "/isi/dokumen")
	var pngBuf bytes.Buffer
	_ = png.Encode(&pngBuf, image.NewGray(image.Rect(0, 0, 2, 2)))
	pngBuf.WriteString("KK " + s["nik pendaftar"])
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	_ = mw.WriteField("csrf_token", strings.ReplaceAll(parent.csrf, "&#43;", "+"))
	fw, _ := mw.CreateFormFile("file", "kk.png")
	_, _ = fw.Write(pngBuf.Bytes())
	_ = mw.Close()
	req := httptest.NewRequest(http.MethodPost, base+"/dokumen/kk", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", "http://example.com")
	for _, c := range parent.cookies {
		req.AddCookie(c)
	}
	upload := httptest.NewRecorder()
	app.ServeHTTP(upload, req)
	for _, c := range upload.Result().Cookies() {
		parent.cookies[c.Name] = c
	}
	expect(upload, http.StatusSeeOther, "upload kk")
	parent.get(base + "/isi/kirim")
	expect(parent.do(http.MethodPost, base+"/kirim", url.Values{}), http.StatusSeeOther, "submit")

	// Impor ekspor Dapodik berisi NIK.
	birth := time.Date(2019, 1, 2, 0, 0, 0, 0, time.UTC)
	var file bytes.Buffer
	if err := dapodik.WritePDExport(&file, "SD", "TU", []dapodik.ExportRow{{Input: dapodik.Input{
		Student: students.Student{FullName: "Impor Privasi", NIS: "PRIV-1", Gender: "P", BirthDate: &birth},
		Profile: students.ProfileForTest(map[string]string{"nik": s["nik siswa (impor PD)"]}),
	}}}, time.Now()); err != nil {
		t.Fatal(err)
	}
	admin.get("/dapodik/impor")
	body.Reset()
	mw = multipart.NewWriter(&body)
	_ = mw.WriteField("csrf_token", strings.ReplaceAll(admin.csrf, "&#43;", "+"))
	fw, _ = mw.CreateFormFile("file", "pd.xlsx")
	_, _ = fw.Write(file.Bytes())
	_ = mw.Close()
	req = httptest.NewRequest(http.MethodPost, "/dapodik/impor", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Origin", "http://example.com")
	for _, c := range admin.cookies {
		req.AddCookie(c)
	}
	preview := httptest.NewRecorder()
	app.ServeHTTP(preview, req)
	expect(preview, http.StatusOK, "import preview")
	for _, c := range preview.Result().Cookies() {
		admin.cookies[c.Name] = c
	}
	if m := csrfField.FindStringSubmatch(preview.Body.String()); m != nil {
		admin.csrf = m[1]
	}
	expect(admin.do(http.MethodPost, "/dapodik/impor/terapkan", url.Values{}), http.StatusOK, "import apply")

	// 1. Database: setiap kolom teks, json, dan bytea di skema public.
	cols, err := tx.Query(ctx, `SELECT table_name, column_name, data_type FROM information_schema.columns
		WHERE table_schema = 'public' AND data_type IN ('text', 'character varying', 'jsonb', 'json', 'bytea', 'ARRAY', 'USER-DEFINED')`)
	if err != nil {
		t.Fatal(err)
	}
	type col struct{ table, column, typ string }
	var all []col
	for cols.Next() {
		var c col
		_ = cols.Scan(&c.table, &c.column, &c.typ)
		all = append(all, c)
	}
	cols.Close()
	if len(all) < 100 {
		t.Fatalf("only %d columns scanned; scan query is probably wrong", len(all))
	}
	ident := regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	for _, c := range all {
		if !ident.MatchString(c.table) || !ident.MatchString(c.column) {
			continue
		}
		cond := `"` + c.column + `"::text LIKE '%' || $1 || '%'`
		if c.typ == "bytea" {
			cond = `position(convert_to($1, 'UTF8') in "` + c.column + `") > 0`
		}
		for label, v := range s {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM "`+c.table+`" WHERE `+cond, v).Scan(&n); err != nil {
				t.Fatalf("scan %s.%s: %v", c.table, c.column, err)
			}
			if n > 0 {
				t.Errorf("%s found as plaintext in %s.%s", label, c.table, c.column)
			}
		}
	}
	// 2. Log aplikasi.
	for label, v := range s {
		if strings.Contains(logs.String(), v) {
			t.Errorf("%s found in application log", label)
		}
	}
	// 3. File di storage (dokumen dan file impor sementara).
	_ = filepath.WalkDir(storeDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, _ := os.ReadFile(path)
		for label, v := range s {
			if bytes.Contains(b, []byte(v)) {
				t.Errorf("%s found in stored file %s", label, filepath.Base(path))
			}
		}
		return nil
	})
	// Data tetap bisa dibaca dengan kunci yang benar.
	prof, err := students.NewProfileService(tx, pii.DevKeyring()).Get(ctx, a.ID, student)
	if err != nil || prof.Secret("nik").Reveal() != s["nik siswa (form)"] {
		t.Errorf("profile readback = %q, %v", prof.Secret("nik").Reveal(), err)
	}
}
