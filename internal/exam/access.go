// Package exam adalah modul ujian online (MVP 4).
package exam

import (
	"slices"
	"time"
)

// Action adalah aksi di modul ujian yang aksesnya diatur matriks (T125).
type Action string

const (
	// ActBank: membuka bank soal mapel dan memakai/menambah soalnya.
	ActBank Action = "bank"
	// ActQuestionEdit: mengubah atau mengarsipkan soal yang sudah ada.
	// Keputusan 7: hanya pembuat soal dan admin_tu.
	ActQuestionEdit Action = "question_edit"
	// ActClassExam: ujian/ulangan untuk kelas yang diampu.
	ActClassExam Action = "class_exam"
	// ActAgenda: agenda PTS/PAS satu sekolah (ruang, sesi, pengawas).
	ActAgenda Action = "agenda"
	// ActAdmissionTest: tes seleksi pendaftaran.
	ActAdmissionTest Action = "admission_test"
	// ActProctor: layar pengawas sebuah sesi.
	ActProctor Action = "proctor"
	// ActGrade: koreksi esai.
	ActGrade Action = "grade"
	// ActSendToReport: kirim nilai ujian ke rapor.
	ActSendToReport Action = "send_to_report"
)

// Level adalah hasil cek akses.
type Level int

const (
	// None: tidak boleh.
	None Level = iota
	// Read: hanya melihat.
	Read
	// Write: boleh melihat dan mengubah.
	Write
)

// Kolom matriks akses.
type column int

const (
	colAdmin column = iota
	colCoordinator
	colTeacher
	colProctor
	colPrincipal
	numColumns
)

// cell adalah satu sel matriks: tingkat akses dan syarat sumber daya.
type cell struct {
	level Level
	// scope membatasi sel ke sumber daya tertentu (mis. mapel yang diampu).
	scope scope
}

type scope int

const (
	scopeAll             scope = iota
	scopeOwnSubject            // mapel yang diampu
	scopeOwnClass              // kelas+mapel yang diampu
	scopeOwner                 // milik sendiri (pembuat)
	scopeOwnerOrAssigned       // milik sendiri atau ditugaskan
	scopeOwnSession            // sesi yang diawasi, dalam jendela waktu
)

var (
	no      = cell{level: None}
	all     = cell{level: Write}
	read    = cell{level: Read}
	subject = cell{level: Write, scope: scopeOwnSubject}
	class   = cell{level: Write, scope: scopeOwnClass}
	owner   = cell{level: Write, scope: scopeOwner}
	ownerOr = cell{level: Write, scope: scopeOwnerOrAssigned}
	session = cell{level: Write, scope: scopeOwnSession}
)

// Matrix adalah matriks akses modul ujian (T125), sama dengan tabel di
// doc/tasks_schoolos_mvp4/14-fondasi/T125. Kolom: admin_tu, koordinator,
// guru mapel, pengawas sesi, kepala sekolah.
var Matrix = map[Action][numColumns]cell{
	ActBank:          {all, all, subject, no, read},
	ActQuestionEdit:  {all, owner, owner, no, no},
	ActClassExam:     {all, all, class, no, read},
	ActAgenda:        {all, all, no, no, read},
	ActAdmissionTest: {all, no, no, no, read},
	ActProctor:       {all, all, no, session, read},
	ActGrade:         {all, all, ownerOr, no, no},
	ActSendToReport:  {no, no, owner, no, no},
}

// Jendela hak pengawas: dari 30 menit sebelum sesi sampai 2 jam sesudahnya.
const (
	ProctorBefore = 30 * time.Minute
	ProctorAfter  = 2 * time.Hour
)

// ProctorAssignment adalah penugasan pengawas untuk satu sesi (T134).
type ProctorAssignment struct {
	SessionID  string
	Start, End time.Time
}

// Active melaporkan apakah hak pengawas berlaku pada now.
func (p ProctorAssignment) Active(now time.Time) bool {
	return !now.Before(p.Start.Add(-ProctorBefore)) && !now.After(p.End.Add(ProctorAfter))
}

// Actor adalah fakta tentang pengguna yang dibutuhkan cek akses. Dibuat
// dengan LoadActor (dari database) atau langsung di uji.
type Actor struct {
	UserID      string
	Roles       []string
	Coordinator bool // tugas koordinator_ujian di tahun ajaran aktif
	// Subjects adalah mapel yang diampu di tahun ajaran aktif.
	Subjects []string
	// Classes berisi pasangan "kelas|mapel" yang diampu.
	Classes  []string
	Proctors []ProctorAssignment
}

// Resource adalah sumber daya yang dicek. Field kosong berarti tidak
// relevan untuk aksi itu.
type Resource struct {
	SubjectID string
	ClassID   string
	OwnerID   string   // pembuat soal/ujian
	Assigned  []string // pengguna yang ditugaskan (mis. pengoreksi)
	SessionID string
}

const (
	roleAdmin     = "admin_tu"
	roleTeacher   = "guru"
	rolePrincipal = "kepala_sekolah"
)

// Can mengembalikan tingkat akses tertinggi a untuk aksi act pada r pada
// waktu now. Bila beberapa kolom berlaku (mis. guru sekaligus koordinator),
// yang tertinggi menang.
func (a Actor) Can(act Action, r Resource, now time.Time) Level {
	row, ok := Matrix[act]
	if !ok {
		return None
	}
	best := None
	try := func(col column) {
		if lv := a.apply(row[col], r, now); lv > best {
			best = lv
		}
	}
	if slices.Contains(a.Roles, roleAdmin) {
		try(colAdmin)
	}
	if slices.Contains(a.Roles, rolePrincipal) {
		try(colPrincipal)
	}
	if slices.Contains(a.Roles, roleTeacher) {
		try(colTeacher)
		if a.Coordinator {
			try(colCoordinator)
		}
	}
	// Pengawas tidak harus guru (mis. staf TU); haknya dari penugasan sesi.
	try(colProctor)
	return best
}

// Allowed adalah singkatan Can(...) >= want.
func (a Actor) Allowed(act Action, r Resource, now time.Time, want Level) bool {
	return a.Can(act, r, now) >= want
}

func (a Actor) apply(c cell, r Resource, now time.Time) Level {
	if c.level == None {
		return None
	}
	switch c.scope {
	case scopeOwnSubject:
		if r.SubjectID == "" || !slices.Contains(a.Subjects, r.SubjectID) {
			return None
		}
	case scopeOwnClass:
		if r.ClassID == "" || !slices.Contains(a.Classes, r.ClassID+"|"+r.SubjectID) {
			return None
		}
	case scopeOwner:
		if r.OwnerID == "" || r.OwnerID != a.UserID {
			return None
		}
	case scopeOwnerOrAssigned:
		if a.UserID == "" || (r.OwnerID != a.UserID && !slices.Contains(r.Assigned, a.UserID)) {
			return None
		}
	case scopeOwnSession:
		ok := false
		for _, p := range a.Proctors {
			if p.SessionID == r.SessionID && r.SessionID != "" && p.Active(now) {
				ok = true
			}
		}
		if !ok {
			return None
		}
	}
	return c.level
}

// CanAny melaporkan apakah a punya akses minimal want ke aksi act untuk
// sumber daya apa pun, mis. untuk menampilkan menu.
func (a Actor) CanAny(act Action, want Level) bool {
	row, ok := Matrix[act]
	if !ok {
		return false
	}
	has := func(col column) bool { return row[col].level >= want }
	switch {
	case slices.Contains(a.Roles, roleAdmin) && has(colAdmin):
		return true
	case slices.Contains(a.Roles, rolePrincipal) && has(colPrincipal):
		return true
	case slices.Contains(a.Roles, roleTeacher) && a.Coordinator && has(colCoordinator):
		return true
	case slices.Contains(a.Roles, roleTeacher) && has(colTeacher):
		return true
	case len(a.Proctors) > 0 && has(colProctor):
		return true
	}
	return false
}
