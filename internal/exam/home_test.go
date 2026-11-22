package exam

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func todoTexts(list []ToDo) string {
	var s []string
	for _, x := range list {
		s = append(s, x.Text+" @ "+x.Href)
	}
	return strings.Join(s, "\n")
}

// "Perlu tindakan" (T158): draf, hasil belum dibuka, nilai belum dikirim,
// agenda belum diumumkan; hilang setelah ditindaklanjuti.
func TestMyToDo(t *testing.T) {
	e := newSecEnv(t)
	ctx := context.Background()
	var examID string
	var ends time.Time
	if err := e.tx.QueryRow(ctx, `SELECT exam_id::text, ends_at FROM exam_sessions WHERE id = $1`, e.sess).Scan(&examID, &ends); err != nil {
		t.Fatal(err)
	}
	owner := e.owner

	// Saat sesi berjalan: hanya ujian draf fixture milik guru.
	list, err := MyToDo(ctx, e.tx, owner, false, e.clock)
	if err != nil {
		t.Fatal(err)
	}
	got := todoTexts(list)
	if !strings.Contains(got, "Ujian masih draf @ /ujian/paket/"+e.a.ExamID+"/atur/") || strings.Contains(got, "Hasil belum dibuka") {
		t.Fatalf("while running:\n%s", got)
	}

	// Sesi selesai, satu attempt bernilai final: hasil belum dibuka dan
	// nilai belum dikirim ke rapor.
	if rec := e.form("hpA", "/u/a/"+e.attA+"/kumpul", url.Values{}); rec.Code != http.StatusSeeOther {
		t.Fatalf("submit = %d", rec.Code)
	}
	if _, err := e.tx.Exec(ctx, `UPDATE exam_attempts SET score_status = 'final' WHERE id = $1`, e.attA); err != nil {
		t.Fatal(err)
	}
	after := ends.Add(time.Hour)
	got = func() string {
		list, err := MyToDo(ctx, e.tx, owner, false, after)
		if err != nil {
			t.Fatal(err)
		}
		return todoTexts(list)
	}()
	for _, want := range []string{"Hasil belum dibuka untuk siswa @ /ujian/paket/" + examID + "/hasil", "Nilai belum dikirim ke rapor @ /ujian/paket/" + examID + "/rapor"} {
		if !strings.Contains(got, want) {
			t.Errorf("after end missing %q:\n%s", want, got)
		}
	}
	// Hasil dibuka: barisnya hilang. Lewat 60 hari: tidak diingatkan lagi.
	if _, err := e.tx.Exec(ctx, `UPDATE exams SET results_released_at = $2, results_released_by = $3 WHERE id = $1`, examID, after, owner.UserID); err != nil {
		t.Fatal(err)
	}
	if list, _ := MyToDo(ctx, e.tx, owner, false, after); strings.Contains(todoTexts(list), "Hasil belum dibuka") {
		t.Error("released results still listed")
	}
	if list, _ := MyToDo(ctx, e.tx, owner, false, after.Add(61*24*time.Hour)); strings.Contains(todoTexts(list), "Nilai belum dikirim") {
		t.Error("old exam still reminded")
	}
	// Guru lain tidak melihat ujian milik guru ini.
	if list, _ := MyToDo(ctx, e.tx, StaffActor{Actor: Actor{UserID: e.a.Treasurer.ID, Roles: []string{"guru"}}, SchoolID: e.a.ID}, false, after); len(list) != 0 {
		t.Errorf("other teacher sees:\n%s", todoTexts(list))
	}

	// Agenda yang mulai hari ini dan belum diumumkan: untuk pengelola agenda,
	// paling atas.
	var eventID string
	if err := e.tx.QueryRow(ctx, `INSERT INTO exam_events (school_id, name, purpose, starts_on, ends_on, grades, created_by)
		VALUES ($1, 'PAS Uji', 'pas', $2::date, $2::date + 3, '{1}', $3) RETURNING id`, e.a.ID, after, e.a.Admin.ID).Scan(&eventID); err != nil {
		t.Fatal(err)
	}
	admin := e.staff(e.a.Admin.ID, "admin_tu")
	list, err = MyToDo(ctx, e.tx, admin, false, after)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 || list[0].Href != "/ujian/agenda/"+eventID || list[0].Text != "Jadwal belum diumumkan ke siswa" {
		t.Errorf("admin to-do:\n%s", todoTexts(list))
	}
	if list, _ := MyToDo(ctx, e.tx, owner, false, after); strings.Contains(todoTexts(list), "Jadwal belum diumumkan") {
		t.Error("teacher sees agenda to-do")
	}
	if ev, err := TodayEvents(ctx, e.tx, e.a.ID, after); err != nil || len(ev) != 1 || ev[0].ID != eventID {
		t.Errorf("today events = %v, %v", ev, err)
	}
}

// Beranda modul per peran (T158).
func TestExamHomePage(t *testing.T) {
	e := newEnv(t)
	teacher := e.get("/ujian/", e.a.Teacher).Body.String()
	for _, want := range []string{"Hari ini", "Perlu tindakan", "Ujian masih draf", `href="/ujian/paket/baru">Buat ujian`} {
		if !strings.Contains(teacher, want) {
			t.Errorf("teacher home missing %q", want)
		}
	}
	principal := e.get("/ujian/", e.a.Principal).Body.String()
	if strings.Contains(principal, "Buat ujian") || strings.Contains(principal, "Ujian masih draf") || !strings.Contains(principal, "Tidak ada yang perlu ditindaklanjuti") {
		t.Error("principal home shows write actions or others' drafts")
	}
	// Sekolah dengan ujian sungguhan: tanpa panduan "mulai".
	if strings.Contains(teacher, "Mulai memakai ujian online") {
		t.Error("fresh-school guide shown while exams exist")
	}
}
