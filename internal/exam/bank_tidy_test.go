package exam

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Header detail bank (T161): satu tombol utama, satu tombol kedua, menu
// "Lainnya"; semua aksi lama tetap tercapai; aksi soal terpilih di menu
// "Cetak & periksa" dengan jumlah soal; aksi per soal di menu titik tiga.
func TestBankDetailActions(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	owner := e.staff(e.a.Teacher.ID, "guru")
	for range 3 {
		if _, err := CreateQuestion(ctx, e.tx, owner, e.bank.ID, sampleQuestion(KindMC), now); err != nil {
			t.Fatal(err)
		}
	}
	path := "/ujian/bank/" + e.bank.ID
	body := e.getAs(path, e.a.Admin.ID, "admin_tu").Body.String()
	i, j := strings.Index(body, `<div class="page-header">`), strings.Index(body, `class="filter-bar`)
	if i < 0 || j < i {
		t.Fatal("page header not found")
	}
	header := body[i:j]
	if n := strings.Count(header, "btn-primary"); n != 1 || !strings.Contains(header, `btn btn-primary" href="`+path+`/soal/baru">Tambah soal`) {
		t.Errorf("header primary buttons = %d", n)
	}
	if !strings.Contains(header, `btn btn-ghost" href="`+path+`/impor">Impor`) || !strings.Contains(header, ">Lainnya</summary>") {
		t.Error("header lacks Impor or Lainnya")
	}
	for _, want := range []string{"/stimulus/baru", "/ekspor/soal.xlsx", "/ekspor/moodle.xml", "/ubah", "/arsip"} {
		if !strings.Contains(header, path+want) {
			t.Errorf("header lacks %s", want)
		}
	}
	if !strings.Contains(header, "/ujian/tp/?mapel="+e.a.SubjectID) {
		t.Error("header lacks Tujuan pembelajaran")
	}
	for _, want := range []string{"Cetak &amp; periksa", "Semua 3 soal sesuai filter", "docs('kisi-kisi')", "docs('kartu-soal')", "docs('mutu')", `aria-label="Aksi soal"`, `class="question-link"`} {
		if !strings.Contains(body, want) {
			t.Errorf("detail lacks %q", want)
		}
	}
	// Kepala sekolah: tanpa tombol tulis, tetap bisa membuka TP dan cetak.
	body = e.getAs(path, e.a.Principal.ID, "kepala_sekolah").Body.String()
	if strings.Contains(body, path+"/soal/baru") || strings.Contains(body, path+"/ubah") || !strings.Contains(body, "Cetak &amp; periksa") {
		t.Error("principal sees write actions or lacks print menu")
	}
}

// Bank paket bawaan bertanda contoh, baik dibuat baru maupun lewat migrasi
// untuk sekolah yang sudah punya (T161).
func TestPracticeBankSample(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	e.tx.Exec(ctx, `UPDATE exams SET archived_at = now() WHERE school_id = $1 AND builtin`, e.a.ID)
	pr, err := EnsurePractice(ctx, e.tx, e.owner, mr(t), now)
	if err != nil {
		t.Fatal(err)
	}
	bank := func() (name string, sample bool) {
		t.Helper()
		if err := e.tx.QueryRow(ctx, `SELECT DISTINCT b.name, b.sample FROM exam_items i
			JOIN exam_questions q ON q.id = i.question_id JOIN exam_banks b ON b.id = q.bank_id
			WHERE i.exam_id = $1`, pr.ID).Scan(&name, &sample); err != nil {
			t.Fatal(err)
		}
		return
	}
	if name, sample := bank(); name != PracticeBankName || !sample {
		t.Errorf("new practice bank = %q sample %v", name, sample)
	}
	// Sekolah lama: nama dan tanda sebelum T161, lalu migrasi 088.
	if _, err := e.tx.Exec(ctx, `UPDATE exam_banks SET name = 'Latihan Antarmuka (contoh)', sample = false
		WHERE id IN (SELECT q.bank_id FROM exam_items i JOIN exam_questions q ON q.id = i.question_id WHERE i.exam_id = $1)`, pr.ID); err != nil {
		t.Fatal(err)
	}
	up, err := os.ReadFile("../../db/migrations/000088_practice_bank_sample.up.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.tx.Exec(ctx, string(up)); err != nil {
		t.Fatal(err)
	}
	if name, sample := bank(); name != PracticeBankName || !sample {
		t.Errorf("migrated practice bank = %q sample %v", name, sample)
	}
}

// Saringan mapel di daftar bank; bank contoh di urutan akhir (T161).
func TestListBanksSubjectFilter(t *testing.T) {
	e := newBankEnv(t)
	ctx := context.Background()
	admin := e.staff(e.a.Admin.ID, "admin_tu")
	var art string
	if err := e.tx.QueryRow(ctx, `INSERT INTO subjects (school_id, name) VALUES ($1, 'Seni Budaya') RETURNING id`, e.a.ID).Scan(&art); err != nil {
		t.Fatal(err)
	}
	sample, err := CreateBank(ctx, e.tx, admin, BankForm{Name: "Aljabar (contoh)", SubjectID: e.a.SubjectID, GradeLevel: "7"}, now)
	if err != nil {
		t.Fatal(err)
	}
	e.tx.Exec(ctx, `UPDATE exam_banks SET sample = true WHERE id = $1`, sample.ID)
	if _, err := CreateBank(ctx, e.tx, admin, BankForm{Name: "Seni 7", SubjectID: art, GradeLevel: "7"}, now); err != nil {
		t.Fatal(err)
	}
	all, err := ListBanks(ctx, e.tx, e.a.ID, admin.Actor, BankFilter{}, now)
	if err != nil || len(all) < 3 {
		t.Fatalf("all banks = %d, %v", len(all), err)
	}
	if !all[len(all)-1].Sample {
		t.Errorf("last bank = %s, want the sample bank", all[len(all)-1].Name)
	}
	only, _ := ListBanks(ctx, e.tx, e.a.ID, admin.Actor, BankFilter{SubjectID: art}, now)
	if len(only) != 1 || only[0].Name != "Seni 7" {
		t.Errorf("art banks = %+v", only)
	}
	subs, err := ListBankSubjects(ctx, e.tx, e.a.ID, admin.Actor, now)
	if err != nil || len(subs) != 2 {
		t.Errorf("bank subjects = %+v, %v", subs, err)
	}
	if body := e.getAs("/ujian/bank/?mapel="+art, e.a.Admin.ID, "admin_tu").Body.String(); !strings.Contains(body, "Seni 7") || strings.Contains(body, "Matematika 7") || !strings.Contains(body, `name="mapel"`) {
		t.Error("subject filter page")
	}
}
