package exam

import (
	"context"
	"strings"
	"testing"

	"akra/internal/testutil"
)

// TestEmptyStatesGuide membuka halaman modul ujian di sekolah baru tanpa
// data: setiap halaman menampilkan panduan langkah dan aksi (T154).
func TestEmptyStatesGuide(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	one := func(sql string, args ...any) string {
		t.Helper()
		var id string
		if err := e.tx.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			t.Fatalf("%v: %s", err, sql)
		}
		return id
	}
	sid := one(`INSERT INTO schools (name, code) VALUES ('Sekolah Baru', 'test-kosong') RETURNING id`)
	one(`INSERT INTO academic_years (school_id, name, start_date, end_date, is_active) VALUES ($1, '2026/2027', '2026-07-01', '2027-06-30', true) RETURNING id`, sid)
	user := func(role string) testutil.User {
		id := one(`INSERT INTO users (school_id, email, password_hash, full_name, is_active) VALUES ($1, $2, 'x', $3, true) RETURNING id`, sid, role+"@kosong.test", "Pengguna "+role)
		one(`INSERT INTO user_roles (user_id, role_id) SELECT $1, id FROM roles WHERE code = $2 RETURNING user_id`, id, role)
		return testutil.User{ID: id, Role: role}
	}
	admin, teacher := user("admin_tu"), user("guru")
	if err := SetModule(ctx, e.tx, sid, Module{Enabled: true, Release: R3}, "uji", nil); err != nil {
		t.Fatal(err)
	}
	e.schoolID = sid

	cases := []struct {
		path string
		u    testutil.User
		want []string // teks wajib selain penanda panduan
	}{
		{"/ujian/paket/", admin, []string{"Belum ada ujian", `href="/ujian/latihan"`}},
		{"/ujian/agenda", admin, []string{"Belum ada agenda", `href="/ujian/ruang"`}},
		{"/ujian/ruang", admin, []string{"Belum ada ruang ujian", "Tambah ruang pertama"}},
		{"/ujian/tp/", admin, []string{"Belum ada mapel"}},
		{"/ujian/telaah", admin, nil},
		{"/ujian/sesi-saya", teacher, []string{"/ujian/panduan/pengawas.pdf", "/ujian/bantuan"}},
		{"/ujian/latihan", teacher, []string{"Tidak ada kelas yang Anda ajar", "Belum ada kelas yang membuka latihan"}},
		{"/ujian/bank/", teacher, []string{"Mulai bank soal", `href="/pengaturan/penugasan"`}},
	}
	for _, c := range cases {
		rec := e.get(c.path, c.u)
		body := rec.Body.String()
		if rec.Code != 200 {
			t.Errorf("%s = %d", c.path, rec.Code)
			continue
		}
		if !strings.Contains(body, "empty-guide") {
			t.Errorf("%s: no guided empty state", c.path)
		}
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: missing %q", c.path, w)
			}
		}
	}
	// Admin membuka bank soal: bank contoh sudah ada, bukan halaman kosong.
	if rec := e.get("/ujian/bank/", admin); !strings.Contains(rec.Body.String(), "Contoh — Matematika") {
		t.Errorf("admin bank list without samples: %d", rec.Code)
	}
}
