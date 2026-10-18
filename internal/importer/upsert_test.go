package importer

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"testing"
)

// Uji impor idempoten (T57).

func (f *uploadFixture) importNow(t *testing.T, rows ...[]any) Batch {
	t.Helper()
	id := f.uploadAndValidate(t, rows...)
	if rec := f.runBatch(t, id); rec.Code != http.StatusSeeOther {
		t.Fatalf("run: status = %d", rec.Code)
	}
	return f.batch(t, id)
}

// Impor file yang sama dua kali: impor kedua 0 siswa baru, semua baris
// "diperbarui", tanpa wali atau enrollment ganda.
func TestIdempotent_SameFileTwice(t *testing.T) {
	f := newUploadFixture(t)
	noPhone := student("003", "Citra")
	noPhone[ColGuardianName], noPhone[ColGuardianPhone] = "Ibu Citra", "" // wali tanpa telepon
	rows := [][]any{student("001", "Ani"), {}, student("002", "Budi"), noPhone}

	first := f.importNow(t, rows...)
	if first.CreatedCount != 3 || first.UpdatedCount != 0 || first.SkippedRows != 1 {
		t.Fatalf("first import = %+v", first)
	}
	counts := func() string {
		return fmt.Sprint(
			f.count(t, `SELECT count(*) FROM students WHERE school_id = $1`, f.schoolA),
			f.count(t, `SELECT count(*) FROM guardians WHERE school_id = $1`, f.schoolA),
			f.count(t, `SELECT count(*) FROM student_guardians WHERE school_id = $1`, f.schoolA),
			f.count(t, `SELECT count(*) FROM enrollments WHERE school_id = $1`, f.schoolA))
	}
	before := counts()

	second := f.importNow(t, rows...)
	if second.Status != StatusDone || second.CreatedCount != 0 || second.UpdatedCount != 3 || second.FailedCount != 0 || second.SkippedRows != 1 {
		t.Errorf("second import = %+v", second)
	}
	if n := f.count(t, `SELECT count(*) FROM import_rows WHERE batch_id = $1 AND result = 'diperbarui'`, second.ID); n != 3 {
		t.Errorf("rows marked diperbarui = %d", n)
	}
	if after := counts(); after != before {
		t.Errorf("students/guardians/links/enrollments: %s -> %s", before, after)
	}
}

// Wali dicocokkan dengan nama dan telepon; tanpa telepon hanya wali yang
// sudah tertaut ke siswa itu.
func TestIdempotent_GuardianMatching(t *testing.T) {
	f := newUploadFixture(t)
	a := student("001", "Ani")
	a[ColGuardianName], a[ColGuardianPhone] = "Siti", "0812-1111-2222"
	b := student("002", "Budi")
	b[ColGuardianName], b[ColGuardianPhone] = "SITI", "+62 812 1111 2222" // sama setelah normalisasi
	c := student("003", "Citra")
	c[ColGuardianName], c[ColGuardianPhone] = "Siti", "0813-9999-0000" // telepon beda: orang lain
	d := student("004", "Dodi")
	d[ColGuardianName], d[ColGuardianPhone], d[ColGuardianEmail] = "Siti", "", "" // tanpa telepon: wali baru
	f.importNow(t, a, b, c, d)
	if n := f.count(t, `SELECT count(*) FROM guardians WHERE school_id = $1`, f.schoolA); n != 3 {
		t.Errorf("guardians = %d, want 3", n)
	}
	if n := f.count(t, `SELECT count(DISTINCT student_id) FROM student_guardians sg JOIN guardians g ON g.id = sg.guardian_id
		WHERE g.school_id = $1 AND g.phone = '+6281211112222'`, f.schoolA); n != 2 {
		t.Errorf("students sharing the guardian = %d", n)
	}
	// Impor ulang Dodi (wali tanpa telepon) memakai wali yang sudah tertaut.
	f.importNow(t, d)
	if n := f.count(t, `SELECT count(*) FROM guardians WHERE school_id = $1`, f.schoolA); n != 3 {
		t.Errorf("guardians after re-import = %d", n)
	}
	// Telepon baru melengkapi wali yang sudah ada, email kosong tidak menghapus.
	d[ColGuardianPhone] = "0857-0000-1111"
	f.importNow(t, d)
	var phone, email string
	if err := f.tx.QueryRow(context.Background(), `SELECT coalesce(g.phone, ''), coalesce(g.email, '') FROM guardians g
		JOIN student_guardians sg ON sg.guardian_id = g.id JOIN students s ON s.id = sg.student_id
		WHERE s.school_id = $1 AND s.nis = '004'`, f.schoolA).Scan(&phone, &email); err != nil || phone != "+6285700001111" {
		t.Errorf("guardian phone = %q, %v", phone, err)
	}
	if n := f.count(t, `SELECT count(*) FROM guardians WHERE school_id = $1`, f.schoolA); n != 3 {
		t.Errorf("guardians after phone update = %d", n)
	}
}

// Siswa yang pindah kelas lewat impor punya tepat satu enrollment aktif,
// juga jika dipindah pada hari yang sama dengan impor pertama.
func TestIdempotent_ClassChange(t *testing.T) {
	f := newUploadFixture(t)
	r := student("001", "Ani")
	f.importNow(t, r)
	r[ColClass] = "1B"
	if b := f.importNow(t, r); b.UpdatedCount != 1 || b.FailedCount != 0 {
		t.Fatalf("move import = %+v", b)
	}
	if n := f.count(t, `SELECT count(*) FROM enrollments e JOIN students s ON s.id = e.student_id WHERE s.nis = '001' AND s.school_id = $1 AND e.end_date IS NULL`, f.schoolA); n != 1 {
		t.Errorf("active enrollments = %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM enrollments e JOIN students s ON s.id = e.student_id
		WHERE s.nis = '001' AND s.school_id = $1 AND e.end_date IS NULL AND e.class_id = $2`, f.schoolA, f.class1B); n != 1 {
		t.Error("student is not active in 1B")
	}
	// Impor ulang ke kelas yang sama tidak menambah enrollment.
	total := f.count(t, `SELECT count(*) FROM enrollments WHERE school_id = $1`, f.schoolA)
	f.importNow(t, r)
	if n := f.count(t, `SELECT count(*) FROM enrollments WHERE school_id = $1`, f.schoolA); n != total {
		t.Errorf("enrollments %d -> %d", total, n)
	}
}

// Job yang berhenti di tengah transaksi batch ke-2 lalu dilanjutkan tidak
// membuat siswa ganda dan tidak menyisakan baris setengah tersimpan.
func TestIdempotent_ResumeAfterMidChunkFailure(t *testing.T) {
	f := newUploadFixture(t)
	ctx := context.Background()
	var rows [][]any
	for i := range 250 {
		r := []any{fmt.Sprintf("%04d", i), fmt.Sprintf("Siswa %d", i), "", "", "1A", "Wali", fmt.Sprintf("0812%08d", i)}
		rows = append(rows, r)
	}
	batchID := f.uploadAndValidate(t, rows...)
	f.runBatch(t, batchID)
	args := ImportStudentsArgs{SchoolID: f.schoolA, BatchID: batchID}

	w := NewImportStudentsWorker(f.tx, slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Baris data di Excel mulai baris 2: transaksi ke-2 berisi baris 102–201.
	w.failAtLine = 150
	if err := w.run(ctx, args); err == nil {
		t.Fatal("injected failure did not stop the job")
	}
	b := f.batch(t, batchID)
	if b.ProcessedRows != runChunkSize || b.Status != StatusRunning {
		t.Errorf("after failure = %+v", b)
	}
	// Baris 102–149 ikut dibatalkan bersama transaksinya.
	if n := f.count(t, `SELECT count(*) FROM students WHERE school_id = $1`, f.schoolA); n != runChunkSize {
		t.Errorf("students after failure = %d, want %d", n, runChunkSize)
	}

	w.failAtLine = 0
	if err := w.run(ctx, args); err != nil {
		t.Fatal(err)
	}
	b = f.batch(t, batchID)
	if b.Status != StatusDone || b.CreatedCount != 250 || b.UpdatedCount != 0 || b.ProcessedRows != 250 {
		t.Errorf("after resume = %+v", b)
	}
	for _, q := range []string{
		`SELECT count(*) FROM students WHERE school_id = $1`,
		`SELECT count(*) FROM guardians WHERE school_id = $1`,
		`SELECT count(*) FROM enrollments WHERE school_id = $1 AND end_date IS NULL`,
	} {
		if n := f.count(t, q, f.schoolA); n != 250 {
			t.Errorf("%s = %d, want 250", q, n)
		}
	}
	if n := f.count(t, `SELECT count(*) FROM audit_logs WHERE school_id = $1 AND action = 'import.run'`, f.schoolA); n != 1 {
		t.Errorf("import.run audits = %d", n)
	}
}
