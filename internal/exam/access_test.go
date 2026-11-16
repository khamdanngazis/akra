package exam

import (
	"fmt"
	"testing"
	"time"
)

var now = time.Date(2027, 3, 10, 1, 0, 0, 0, time.UTC) // 08.00 WIB

const (
	me      = "11111111-1111-1111-1111-111111111111"
	someone = "99999999-9999-9999-9999-999999999999"
	subjA   = "subj-a"
	subjB   = "subj-b"
	classA  = "class-a"
	sessA   = "sess-a"
	sessB   = "sess-b"
)

// Pelaku per kolom matriks. Koordinator adalah guru dengan tugas
// koordinator_ujian yang tidak mengampu mapel di uji ini, sehingga yang
// diuji adalah hak koordinatornya. Pengawas adalah guru tanpa mapel.
var actors = map[string]Actor{
	"admin":       {UserID: me, Roles: []string{roleAdmin}},
	"coordinator": {UserID: me, Roles: []string{roleTeacher}, Coordinator: true},
	"teacher":     {UserID: me, Roles: []string{roleTeacher}, Subjects: []string{subjA}, Classes: []string{classA + "|" + subjA}},
	"proctor": {UserID: me, Roles: []string{roleTeacher}, Proctors: []ProctorAssignment{
		{SessionID: sessA, Start: now.Add(-time.Hour), End: now.Add(time.Hour)}}},
	"principal": {UserID: me, Roles: []string{rolePrincipal}},
	"parent":    {UserID: me, Roles: []string{"orang_tua"}},
}

// Sumber daya per aksi: "own" memenuhi syarat cakupan pelaku (mapel yang
// diampu, milik sendiri, sesi yang diawasi), "other" tidak.
var resources = map[Action]struct{ own, other Resource }{
	ActBank:          {Resource{SubjectID: subjA}, Resource{SubjectID: subjB}},
	ActQuestionEdit:  {Resource{SubjectID: subjA, OwnerID: me}, Resource{SubjectID: subjA, OwnerID: someone}},
	ActClassExam:     {Resource{ClassID: classA, SubjectID: subjA, OwnerID: me}, Resource{ClassID: classA, SubjectID: subjB, OwnerID: someone}},
	ActAgenda:        {Resource{}, Resource{}},
	ActAdmissionTest: {Resource{}, Resource{}},
	ActProctor:       {Resource{SessionID: sessA}, Resource{SessionID: sessB}},
	ActGrade:         {Resource{OwnerID: someone, Assigned: []string{me}}, Resource{OwnerID: someone}},
	ActSendToReport:  {Resource{OwnerID: me}, Resource{OwnerID: someone}},
}

// Harapan per sel, ditulis ulang dari tabel di dokumen T125 (bukan dari
// variabel Matrix): "W" boleh ubah, "R" baca, "-" tolak; "W/-" berarti W
// untuk sumber daya own dan tolak untuk other. Koordinator dan pengawas di
// uji ini juga guru, jadi tetap punya hak guru atas milik sendiri
// (soal, ujian, koreksi), tanpa hak atas milik orang lain.
var want = map[Action]map[string]string{
	ActBank:          {"admin": "W", "coordinator": "W", "teacher": "W/-", "proctor": "-", "principal": "R", "parent": "-"},
	ActQuestionEdit:  {"admin": "W", "coordinator": "W/-", "teacher": "W/-", "proctor": "W/-", "principal": "-", "parent": "-"},
	ActClassExam:     {"admin": "W", "coordinator": "W", "teacher": "W/-", "proctor": "-", "principal": "R", "parent": "-"},
	ActAgenda:        {"admin": "W", "coordinator": "W", "teacher": "-", "proctor": "-", "principal": "R", "parent": "-"},
	ActAdmissionTest: {"admin": "W", "coordinator": "-", "teacher": "-", "proctor": "-", "principal": "R", "parent": "-"},
	ActProctor:       {"admin": "W", "coordinator": "W", "teacher": "-", "proctor": "W/-", "principal": "R", "parent": "-"},
	ActGrade:         {"admin": "W", "coordinator": "W", "teacher": "W/-", "proctor": "W/-", "principal": "-", "parent": "-"},
	ActSendToReport:  {"admin": "-", "coordinator": "W/-", "teacher": "W/-", "proctor": "W/-", "principal": "-", "parent": "-"},
}

func levelCode(l Level) string {
	return map[Level]string{None: "-", Read: "R", Write: "W"}[l]
}

func TestMatrixEveryCell(t *testing.T) {
	if len(want) != len(Matrix) {
		t.Fatalf("test covers %d actions, Matrix has %d", len(want), len(Matrix))
	}
	for act, row := range want {
		res, ok := resources[act]
		if !ok {
			t.Fatalf("no resources for %s", act)
		}
		for name, exp := range row {
			a := actors[name]
			got := levelCode(a.Can(act, res.own, now))
			gotOther := levelCode(a.Can(act, res.other, now))
			var cell string
			switch {
			case got == gotOther:
				cell = got
			default:
				cell = got + "/" + gotOther
			}
			if cell != exp {
				t.Errorf("%s × %s = %s, want %s", act, name, cell, exp)
			}
		}
	}
}

// Koordinator yang juga guru mapel: tetap mendapat hak terbesar, dan hak
// ubah soal tetap hanya untuk soal miliknya (keputusan 7).
func TestCoordinatorWhoTeaches(t *testing.T) {
	a := Actor{UserID: me, Roles: []string{roleTeacher}, Coordinator: true, Subjects: []string{subjA}}
	if got := a.Can(ActBank, Resource{SubjectID: subjB}, now); got != Write {
		t.Errorf("bank other subject = %v, want Write", got)
	}
	if got := a.Can(ActQuestionEdit, Resource{SubjectID: subjA, OwnerID: someone}, now); got != None {
		t.Errorf("edit someone else's question = %v, want None", got)
	}
}

// Koordinator tanpa peran guru (data tidak wajar) tidak mendapat hak.
func TestCoordinatorFlagRequiresTeacher(t *testing.T) {
	a := Actor{UserID: me, Roles: []string{"bendahara"}, Coordinator: true}
	if got := a.Can(ActAgenda, Resource{}, now); got != None {
		t.Errorf("treasurer with coordinator flag = %v, want None", got)
	}
}

func TestProctorWindow(t *testing.T) {
	start := time.Date(2027, 3, 10, 1, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	a := Actor{UserID: me, Roles: []string{roleTeacher}, Proctors: []ProctorAssignment{{SessionID: sessA, Start: start, End: end}}}
	cases := []struct {
		at   time.Time
		want Level
	}{
		{start.Add(-31 * time.Minute), None},
		{start.Add(-30 * time.Minute), Write},
		{start.Add(45 * time.Minute), Write},
		{end.Add(2 * time.Hour), Write},
		{end.Add(2*time.Hour + time.Second), None},
		{end.Add(24 * time.Hour), None},
	}
	for _, c := range cases {
		if got := a.Can(ActProctor, Resource{SessionID: sessA}, c.at); got != c.want {
			t.Errorf("at %s: %v, want %v", c.at.Sub(start), got, c.want)
		}
	}
	if got := a.Can(ActProctor, Resource{}, start); got != None {
		t.Errorf("empty session id = %v, want None", got)
	}
}

func TestUnknownActionDenied(t *testing.T) {
	if got := actors["admin"].Can(Action("hapus_semua"), Resource{}, now); got != None {
		t.Errorf("unknown action = %v", got)
	}
}

func TestCanAny(t *testing.T) {
	cases := []struct {
		actor string
		act   Action
		lv    Level
		want  bool
	}{
		{"coordinator", ActAgenda, Write, true},
		{"teacher", ActAgenda, Read, false},
		{"principal", ActAgenda, Read, true},
		{"principal", ActAgenda, Write, false},
		{"proctor", ActProctor, Write, true},
		{"parent", ActBank, Read, false},
	}
	for _, c := range cases {
		if got := actors[c.actor].CanAny(c.act, c.lv); got != c.want {
			t.Errorf("%s", fmt.Sprintf("%s CanAny(%s, %v) = %v", c.actor, c.act, c.lv, got))
		}
	}
}
