package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/alexedwards/scs/v2/memstore"

	"akra/internal/auth"
	"akra/internal/platform/database"
	"akra/internal/platform/mail"
	"akra/internal/platform/render"
)

// captureMailer menyimpan email yang "dikirim".
type captureMailer struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (m *captureMailer) Send(_ context.Context, msg mail.Message) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sent = append(m.sent, msg)
	return nil
}

func (m *captureMailer) last(t *testing.T) mail.Message {
	t.Helper()
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.sent) == 0 {
		t.Fatal("no email sent")
	}
	return m.sent[len(m.sent)-1]
}

// client menyimpan cookie antar-request dan mengisi token CSRF dari halaman
// terakhir, seperti browser sungguhan.
type client struct {
	t       *testing.T
	app     http.Handler
	cookies map[string]*http.Cookie
	csrf    string
}

var csrfField = regexp.MustCompile(`name="csrf_token" value="([^"]+)"`)

func newClient(t *testing.T, app http.Handler) *client {
	return &client{t: t, app: app, cookies: map[string]*http.Cookie{}}
}

func (b *client) do(method, path string, form url.Values) *httptest.ResponseRecorder {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		if b.csrf != "" {
			form.Set("csrf_token", strings.ReplaceAll(b.csrf, "&#43;", "+"))
		}
		body = strings.NewReader(form.Encode())
	}
	req := httptest.NewRequest(method, path, body)
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", "http://example.com")
	}
	for _, c := range b.cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	b.app.ServeHTTP(rec, req)
	for _, c := range rec.Result().Cookies() {
		if c.MaxAge < 0 {
			delete(b.cookies, c.Name)
		} else {
			b.cookies[c.Name] = c
		}
	}
	if m := csrfField.FindStringSubmatch(rec.Body.String()); m != nil {
		b.csrf = m[1]
	}
	return rec
}

// get membuka halaman (mengambil token CSRF baru).
func (b *client) get(path string) *httptest.ResponseRecorder { return b.do(http.MethodGet, path, nil) }

func (b *client) login(school, email, password string) *httptest.ResponseRecorder {
	b.get("/login")
	return b.do(http.MethodPost, "/login", url.Values{"school_code": {school}, "email": {email}, "password": {password}})
}

func TestGuardianAccount_EndToEnd(t *testing.T) {
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	hasher := auth.NewHasher(auth.PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1})
	adminHash, _ := hasher.HashPassword("Admin-Rahasia-1")
	rd, err := render.New(mustSub(t, "templates"), mustSub(t, "static"), logger)
	if err != nil {
		t.Fatal(err)
	}
	mailer := &captureMailer{}
	app, err := newApp(appDeps{
		db: tx, pinger: pool, sessions: auth.NewSessionManager(memstore.NewWithCleanupInterval(0), false, logger),
		render: rd, static: mustSub(t, "static"), mailer: mailer, hasher: hasher,
		baseURL: "https://akra.test", logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Data: sekolah, admin, siswa dengan tiga wali.
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	schoolID := one(`INSERT INTO schools (name, code) VALUES ('SD Uji Portal', 'test-portal') RETURNING id`)
	one(`WITH u AS (INSERT INTO users (school_id, email, password_hash, full_name) VALUES ($1, 'admin@uji.id', $2, 'Admin') RETURNING id)
		INSERT INTO user_roles SELECT u.id, r.id FROM u, roles r WHERE r.code = 'admin_tu' RETURNING user_id`, schoolID, adminHash)
	studentID := one(`INSERT INTO students (school_id, nis, full_name) VALUES ($1, '001', 'Budi') RETURNING id`, schoolID)
	guardian := func(name, email string) string {
		id := one(`INSERT INTO guardians (school_id, full_name, email) VALUES ($1, $2, nullif($3, '')) RETURNING id`, schoolID, name, email)
		if _, err := tx.Exec(ctx, `INSERT INTO student_guardians (school_id, student_id, guardian_id, relationship) VALUES ($1, $2, $3, 'wali')`, schoolID, studentID, id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	ibu := guardian("Ibu Sari", "sari@contoh.id")
	ayah := guardian("Pak Tanpa Email", "")
	paman := guardian("Paman Joko", "joko@contoh.id")

	userCount := func(guardianID string) int {
		var n int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM users WHERE guardian_id = $1`, guardianID).Scan(&n)
		return n
	}
	flash := func(b *client, path string) string {
		return regexp.MustCompile(`role="status">([^<]*)`).FindStringSubmatch(b.get(path).Body.String() + `role="status">`)[1]
	}

	admin := newClient(t, app)
	if rec := admin.login("test-portal", "admin@uji.id", "Admin-Rahasia-1"); rec.Code != http.StatusSeeOther {
		t.Fatalf("admin login: %d", rec.Code)
	}
	detail := admin.get("/siswa/" + studentID).Body.String()
	if !strings.Contains(detail, "/wali/"+ibu+"/akun") || strings.Contains(detail, "/wali/"+ayah+"/akun") || !strings.Contains(detail, "Tambahkan email untuk membuat akun") {
		t.Error("student detail does not offer account creation only for guardians with email")
	}

	// 1. Membuat akun.
	rec := admin.do(http.MethodPost, "/siswa/"+studentID+"/wali/"+ibu+"/akun", url.Values{})
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("create account: status = %d", rec.Code)
	}
	if msg := flash(admin, "/siswa/"+studentID); !strings.Contains(msg, "Tautan aktivasi dikirim ke sari@contoh.id") {
		t.Errorf("flash = %q", msg)
	}
	var active bool
	var hash, role, accountSchool string
	_ = tx.QueryRow(ctx, `SELECT u.is_active, u.password_hash, r.code, u.school_id FROM users u
		JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id WHERE u.guardian_id = $1`, ibu).Scan(&active, &hash, &role, &accountSchool)
	if active || hash != "" || role != "orang_tua" || accountSchool != schoolID {
		t.Errorf("new account: active=%v hash=%q role=%q school ok=%v", active, hash, role, accountSchool == schoolID)
	}
	invite := mailer.last(t)
	if invite.To != "sari@contoh.id" || !strings.Contains(invite.Text, "Kode sekolah : test-portal") {
		t.Errorf("invite email = %+v", invite)
	}
	link := regexp.MustCompile(`https://akra\.test/aktivasi\?token=\S+`).FindString(invite.Text)
	activationPath := strings.TrimPrefix(link, "https://akra.test")
	if activationPath == "" {
		t.Fatalf("no activation link in email:\n%s", invite.Text)
	}

	// Kriteria: akun yang sama dua kali ditolak.
	admin.get("/siswa/" + studentID)
	admin.do(http.MethodPost, "/siswa/"+studentID+"/wali/"+ibu+"/akun", url.Values{})
	if msg := flash(admin, "/siswa/"+studentID); !strings.Contains(msg, "sudah punya akun") || userCount(ibu) != 1 {
		t.Errorf("second create: flash = %q, accounts = %d", msg, userCount(ibu))
	}
	// Kriteria: wali tanpa email ditolak.
	admin.do(http.MethodPost, "/siswa/"+studentID+"/wali/"+ayah+"/akun", url.Values{})
	if msg := flash(admin, "/siswa/"+studentID); !strings.Contains(msg, "belum punya email") || userCount(ayah) != 0 {
		t.Errorf("no email: flash = %q, accounts = %d", msg, userCount(ayah))
	}

	// Kriteria: akun belum aktif tidak bisa login.
	parent := newClient(t, app)
	for _, pw := range []string{"", "apa saja"} {
		if rec := parent.login("test-portal", "sari@contoh.id", pw); rec.Code != http.StatusUnauthorized {
			t.Errorf("login before activation with %q: status = %d, want 401", pw, rec.Code)
		}
	}

	// Aktivasi.
	page := parent.get(activationPath)
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "test-portal") || page.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("activation page: status = %d", page.Code)
	}
	token := strings.TrimPrefix(activationPath, "/aktivasi?token=")
	if rec := parent.do(http.MethodPost, "/aktivasi", url.Values{"token": {token}, "password": {"Ortu-Rahasia-9"}, "password_confirm": {"beda"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("mismatched confirmation: status = %d", rec.Code)
	}
	rec = parent.do(http.MethodPost, "/aktivasi", url.Values{"token": {token}, "password": {"Ortu-Rahasia-9"}, "password_confirm": {"Ortu-Rahasia-9"}})
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/login" {
		t.Fatalf("activate: status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if msg := flash(parent, "/login"); !strings.Contains(msg, "kode sekolah test-portal") {
		t.Errorf("activation flash = %q", msg)
	}

	// Kriteria: tautan yang sudah dipakai ditolak.
	if rec := parent.get(activationPath); rec.Code != http.StatusBadRequest {
		t.Errorf("reused link: status = %d, want 400", rec.Code)
	}
	if rec := parent.do(http.MethodPost, "/aktivasi", url.Values{"token": {token}, "password": {"Lain-12345"}, "password_confirm": {"Lain-12345"}}); rec.Code != http.StatusBadRequest {
		t.Errorf("reused link POST: status = %d, want 400", rec.Code)
	}

	// Kriteria: setelah aktivasi, login diarahkan ke /portal.
	rec = parent.login("test-portal", "sari@contoh.id", "Ortu-Rahasia-9")
	if rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/portal" {
		t.Fatalf("parent login: status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
	if rec := parent.get("/portal"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Halo, Ibu Sari") {
		t.Errorf("portal: status = %d", rec.Code)
	}
	if rec := parent.get("/"); rec.Code != http.StatusSeeOther || rec.Header().Get("Location") != "/portal" {
		t.Errorf("parent at /: status = %d, location = %q", rec.Code, rec.Header().Get("Location"))
	}
	// Kriteria: orang tua 403 di halaman admin.
	for _, p := range []string{"/siswa", "/siswa/" + studentID, "/pengaturan/tahun-ajaran", "/kelas"} {
		if rec := parent.get(p); rec.Code != http.StatusForbidden {
			t.Errorf("parent GET %s: status = %d, want 403", p, rec.Code)
		}
	}
	// next ke halaman admin diabaikan untuk orang tua.
	parent2 := newClient(t, app)
	parent2.get("/login?next=%2Fsiswa")
	rec = parent2.do(http.MethodPost, "/login", url.Values{"school_code": {"test-portal"}, "email": {"sari@contoh.id"}, "password": {"Ortu-Rahasia-9"}, "next": {"/siswa"}})
	if rec.Header().Get("Location") != "/portal" {
		t.Errorf("parent login with next=/siswa went to %q", rec.Header().Get("Location"))
	}
	// Staf tidak bisa membuka portal.
	if rec := admin.get("/portal"); rec.Code != http.StatusForbidden {
		t.Errorf("admin GET /portal: status = %d, want 403", rec.Code)
	}

	// Kriteria: tautan kedaluwarsa ditolak; kirim ulang memberi tautan baru.
	admin.get("/siswa/" + studentID)
	admin.do(http.MethodPost, "/siswa/"+studentID+"/wali/"+paman+"/akun", url.Values{})
	oldLink := strings.TrimPrefix(regexp.MustCompile(`https://akra\.test(/aktivasi\?token=\S+)`).FindStringSubmatch(mailer.last(t).Text)[1], "")
	if _, err := tx.Exec(ctx, `UPDATE activation_tokens SET expires_at = now() - interval '1 second'
		WHERE user_id = (SELECT id FROM users WHERE guardian_id = $1)`, paman); err != nil {
		t.Fatal(err)
	}
	if rec := parent.get(oldLink); rec.Code != http.StatusBadRequest {
		t.Errorf("expired link: status = %d, want 400", rec.Code)
	}
	admin.get("/siswa/" + studentID)
	admin.do(http.MethodPost, "/siswa/"+studentID+"/wali/"+paman+"/akun/kirim-ulang", url.Values{})
	newLink := regexp.MustCompile(`https://akra\.test(/aktivasi\?token=\S+)`).FindStringSubmatch(mailer.last(t).Text)[1]
	if newLink == oldLink {
		t.Fatal("resend returned the same link")
	}
	if rec := parent.get(newLink); rec.Code != http.StatusOK {
		t.Errorf("resent link: status = %d, want 200", rec.Code)
	}
	// Kirim ulang untuk akun yang sudah aktif ditolak.
	admin.get("/siswa/" + studentID)
	admin.do(http.MethodPost, "/siswa/"+studentID+"/wali/"+ibu+"/akun/kirim-ulang", url.Values{})
	if msg := flash(admin, "/siswa/"+studentID); !strings.Contains(msg, "tidak sedang menunggu aktivasi") {
		t.Errorf("resend for active account: flash = %q", msg)
	}

	// Token hanya tersimpan sebagai hash; audit tercatat.
	var plain int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM activation_tokens WHERE position($1::bytea in token_hash) > 0`, []byte(token)).Scan(&plain)
	var creates, activates, resends int
	_ = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE action = 'guardian_account.create'),
		count(*) FILTER (WHERE action = 'guardian_account.activate'),
		count(*) FILTER (WHERE action = 'guardian_account.resend_activation')
		FROM audit_logs WHERE school_id = $1`, schoolID).Scan(&creates, &activates, &resends)
	if plain != 0 || creates != 2 || activates != 1 || resends != 1 {
		t.Errorf("plaintext tokens = %d, audits create=%d activate=%d resend=%d", plain, creates, activates, resends)
	}
}
