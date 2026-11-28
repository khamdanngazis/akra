// Package module berisi katalog modul AKRA dan status modul per sekolah
// (T163, ADR 0002). Setiap sekolah memakai sebagian modul; modul yang tidak
// dipakai tidak tampil dan tidak bisa dibuka. Modul core selalu aktif.
//
// Status dibaca sekali per request (Middleware) dengan cache per replika
// selama CacheTTL, sehingga perubahan berlaku tanpa login ulang.
package module

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

// ID adalah kode modul.
type ID string

const (
	Core          ID = "core"
	Attendance    ID = "attendance"
	Schedule      ID = "schedule"
	Grades        ID = "grades"
	Billing       ID = "billing"
	Admission     ID = "admission"
	Dapodik       ID = "dapodik"
	Exam          ID = "exam"
	Announcements ID = "announcements"
	Portal        ID = "portal"
)

// Info menjelaskan satu modul di katalog.
type Info struct {
	ID   ID
	Name string
	Desc string
	// Requires: modul yang wajib aktif agar modul ini bisa diaktifkan.
	// Optional: modul yang menambah fitur bila aktif (tidak menghalangi).
	Requires, Optional []ID
	// Routes adalah pola path milik modul, dicocokkan per segmen (lihat
	// Required): "*" cocok dengan satu segmen apa pun, dan segmen terakhir
	// juga cocok dengan nama berakhiran ".ext" (mis. "kehadiran.pdf").
	Routes []string
	// Features adalah fitur di dalam modul (pengaturan sekolah, ADR 0002
	// bagian 3).
	Features []string
	// Default: aktif untuk sekolah yang belum punya baris status modul ini.
	Default bool
}

// Catalog adalah semua modul AKRA dalam urutan tampil.
var Catalog = []Info{
	{ID: Core, Name: "Inti", Desc: "Profil sekolah, tahun ajaran, kelas, siswa dan wali, mapel, penugasan guru, pengguna, impor siswa, dan pengaturan.",
		Routes: []string{"/siswa", "/kelas", "/pengaturan", "/impor/siswa", "/laporan"}, Default: true},
	{ID: Attendance, Name: "Absensi", Desc: "Absensi harian dan per mapel, rekap, dan laporan kehadiran.",
		Requires: []ID{Core}, Routes: []string{"/absensi", "/laporan/kehadiran", "/pengaturan/absensi"}, Features: []string{"Absensi per mapel"}, Default: true},
	{ID: Schedule, Name: "Jadwal", Desc: "Jadwal pelajaran dan jadwal mengajar guru.",
		Requires: []ID{Core}, Routes: []string{"/jadwal", "/portal/siswa/*/jadwal"}, Default: true},
	{ID: Grades, Name: "Nilai & Rapor", Desc: "Komponen nilai, input dan rekap nilai, rapor PDF, dan impor nilai.",
		Requires: []ID{Core}, Optional: []ID{Attendance}, Routes: []string{"/nilai", "/pengaturan/rapor", "/impor/nilai", "/portal/siswa/*/nilai", "/portal/siswa/*/rapor",
			// Fitur modul lain yang menulis atau membaca nilai (T166).
			"/ujian/paket/*/rapor", "/dapodik/nilai"},
		Features: []string{"Skala dan KKM", "Format rapor", "Kebijakan remedial", "Nilai per komponen di portal"}, Default: true},
	{ID: Billing, Name: "Keuangan", Desc: "Jenis tagihan, tagihan massal, pembayaran dan kuitansi, tunggakan, dan audit keuangan.",
		Requires: []ID{Core}, Routes: []string{"/keuangan", "/pengaturan/keuangan", "/laporan/tunggakan", "/portal/siswa/*/tagihan", "/portal/pembayaran"},
		Features: []string{"Ambang persetujuan pembatalan"}, Default: true},
	{ID: Admission, Name: "Pendaftaran", Desc: "Periode dan gelombang pendaftaran, formulir publik, pendaftar, tes, daftar ulang, dan jadikan siswa.",
		Requires: []ID{Core}, Optional: []ID{Billing, Exam}, Routes: []string{"/pendaftaran", "/s/*/daftar", "/s/*/pindah", "/s/*/status"},
		Features: []string{"Format NIS", "Daftar ulang"}, Default: true},
	{ID: Dapodik, Name: "Bantu Dapodik", Desc: "Lembar salin, cek kesiapan, F-PD, impor dan ekspor, serta nilai ke Dapodik.",
		Requires: []ID{Core}, Optional: []ID{Grades}, Routes: []string{"/dapodik"}, Default: true},
	{ID: Exam, Name: "Ujian Online", Desc: "Bank soal, ujian, sesi dan pengawasan, sampai hasil dan analisis.",
		Requires: []ID{Core}, Optional: []ID{Grades, Admission}, Routes: []string{"/ujian", "/u", "/portal/siswa/*/ujian"},
		Features: []string{"Tahap rilis r1–r3", "Telaah soal", "Target kisi-kisi"}},
	{ID: Announcements, Name: "Pengumuman", Desc: "Pengumuman untuk staf dan orang tua.",
		Requires: []ID{Core}, Routes: []string{"/pengumuman", "/portal/pengumuman"}, Default: true},
	{ID: Portal, Name: "Portal Orang Tua", Desc: "Akun wali, aktivasi, dan halaman anak untuk orang tua.",
		Requires: []ID{Core}, Optional: []ID{Attendance, Schedule, Grades, Billing, Exam, Announcements},
		Routes: []string{"/portal", "/aktivasi", "/siswa/*/wali/*/akun"}, Default: true},
}

// Get mengembalikan info modul id.
func Get(id ID) (Info, bool) {
	for _, m := range Catalog {
		if m.ID == id {
			return m, true
		}
	}
	return Info{}, false
}

// Known melaporkan apakah id ada di katalog.
func Known(id ID) bool {
	_, ok := Get(id)
	return ok
}

// Required mengembalikan modul opsional yang wajib aktif untuk membuka path
// (tanpa core), dalam urutan katalog. Satu path bisa milik beberapa modul,
// mis. "/portal/siswa/{id}/tagihan" butuh portal dan billing. Path juga
// boleh berupa pola route chi ("/portal/siswa/{id}/tagihan"): segmen
// "{...}" diperlakukan sebagai nilai apa pun.
func Required(path string) []ID {
	segs := splitPath(path)
	var out []ID
	for _, m := range Catalog {
		if m.ID == Core {
			continue
		}
		for _, pat := range m.Routes {
			if routeMatch(splitPath(pat), segs) {
				out = append(out, m.ID)
				break
			}
		}
	}
	return out
}

// Owner mengembalikan modul paling spesifik untuk path (pola terpanjang),
// atau Core bila path bukan milik modul opsional.
func Owner(path string) ID {
	segs := splitPath(path)
	best, bestLen := Core, 0
	for _, m := range Catalog {
		if m.ID == Core {
			continue
		}
		for _, pat := range m.Routes {
			p := splitPath(pat)
			if len(p) > bestLen && routeMatch(p, segs) {
				best, bestLen = m.ID, len(p)
			}
		}
	}
	return best
}

func splitPath(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool { return r == '/' })
}

// routeMatch melaporkan apakah pola pat cocok dengan awal path segs.
func routeMatch(pat, segs []string) bool {
	if len(pat) > len(segs) {
		return false
	}
	for i, p := range pat {
		s := segs[i]
		switch {
		case p == "*" || p == s:
		case i == len(pat)-1 && strings.HasPrefix(s, p+"."):
		default:
			return false
		}
	}
	return true
}

// Missing mengembalikan modul yang dibutuhkan path tetapi tidak aktif di s
// (kosong bila path boleh dibuka).
func (s Set) Missing(path string) []ID {
	var out []ID
	for _, id := range Required(path) {
		if !s.Has(id) {
			out = append(out, id)
		}
	}
	return out
}

// Set adalah modul yang aktif untuk satu sekolah beserta pilihannya. Nilai
// nol hanya berisi core.
type Set struct {
	on   map[ID]bool
	opts map[ID]map[string]string
}

// NewSet membuat set berisi core dan ids.
func NewSet(ids ...ID) Set {
	s := Set{on: map[ID]bool{}, opts: map[ID]map[string]string{}}
	for _, id := range ids {
		s.on[id] = true
	}
	return s
}

// All mengembalikan set berisi semua modul katalog (untuk uji paket yang
// memasang router tanpa Middleware).
func All() Set {
	s := NewSet()
	for _, m := range Catalog {
		s.on[m.ID] = true
	}
	return s
}

// Defaults mengembalikan set bawaan katalog: semua modul kecuali ujian.
func Defaults() Set {
	s := NewSet()
	for _, m := range Catalog {
		if m.Default {
			s.on[m.ID] = true
		}
	}
	return s
}

// Has melaporkan apakah modul id aktif. Core selalu aktif.
func (s Set) Has(id ID) bool { return id == Core || s.on[id] }

// HasName seperti Has untuk kode modul berupa string (menu, template).
// Kode kosong berarti bukan milik modul mana pun (selalu tampil).
func (s Set) HasName(id string) bool { return id == "" || s.Has(ID(id)) }

// Option mengembalikan pilihan key modul id ("" bila tidak diatur).
func (s Set) Option(id ID, key string) string { return s.opts[id][key] }

// HasOption melaporkan apakah modul id aktif dan pilihan key diatur.
func (s Set) HasOption(id ID, key string) bool { return s.Has(id) && s.opts[id][key] != "" }

// IDs mengembalikan kode modul aktif (termasuk core) dalam urutan katalog.
func (s Set) IDs() []string {
	var out []string
	for _, m := range Catalog {
		if s.Has(m.ID) {
			out = append(out, string(m.ID))
		}
	}
	return out
}

// State adalah status satu modul satu sekolah.
type State struct {
	Enabled bool
	Options map[string]string
	Note    string
}

// CacheTTL adalah lama status modul disimpan di memori per replika.
const CacheTTL = 30 * time.Second

// Now adalah jam untuk cache; diganti di uji (jam palsu).
var Now = time.Now

type entry struct {
	set Set
	at  time.Time
}

var cache = struct {
	sync.Mutex
	m map[string]entry
}{m: map[string]entry{}}

// Forget menghapus status sekolah dari cache replika ini, mis. setelah
// diubah di luar Save.
func Forget(schoolID string) {
	cache.Lock()
	delete(cache.m, schoolID)
	cache.Unlock()
}

// Load mengembalikan modul aktif sekolah. Hasil disimpan di memori replika
// ini selama CacheTTL. Modul tanpa baris memakai bawaan katalog.
func Load(ctx context.Context, q database.DBTX, schoolID string) (Set, error) {
	now := Now()
	cache.Lock()
	e, ok := cache.m[schoolID]
	cache.Unlock()
	if ok && now.Sub(e.at) < CacheTTL {
		return e.set, nil
	}
	s, err := load(ctx, q, schoolID)
	if err != nil {
		return Set{}, err
	}
	cache.Lock()
	cache.m[schoolID] = entry{set: s, at: now}
	cache.Unlock()
	return s, nil
}

func load(ctx context.Context, q database.DBTX, schoolID string) (Set, error) {
	s := Defaults()
	rows, err := q.Query(ctx, `SELECT module, enabled, options FROM school_modules WHERE school_id = $1`, schoolID)
	if err != nil {
		return Set{}, fmt.Errorf("module: load: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id ID
		var on bool
		var raw []byte
		if err := rows.Scan(&id, &on, &raw); err != nil {
			return Set{}, fmt.Errorf("module: load: %w", err)
		}
		if !Known(id) {
			continue
		}
		s.on[id] = on
		if !on {
			delete(s.on, id)
		}
		var opts map[string]string
		if err := json.Unmarshal(raw, &opts); err != nil {
			return Set{}, fmt.Errorf("module: options of %s: %w", id, err)
		}
		if len(opts) > 0 {
			s.opts[id] = opts
		}
	}
	return s, rows.Err()
}

// GetState mengembalikan status modul id sekolah tanpa cache (untuk alat
// pengelola). ok false bila belum ada baris (bawaan katalog berlaku).
func GetState(ctx context.Context, q database.DBTX, schoolID string, id ID) (State, bool, error) {
	var st State
	var raw []byte
	err := q.QueryRow(ctx, `SELECT enabled, options, note FROM school_modules WHERE school_id = $1 AND module = $2`, schoolID, id).Scan(&st.Enabled, &raw, &st.Note)
	if errors.Is(err, pgx.ErrNoRows) {
		info, _ := Get(id)
		return State{Enabled: info.Default}, false, nil
	}
	if err != nil {
		return State{}, false, fmt.Errorf("module: get %s: %w", id, err)
	}
	if err := json.Unmarshal(raw, &st.Options); err != nil {
		return State{}, false, fmt.Errorf("module: options of %s: %w", id, err)
	}
	return st, true, nil
}

// ErrUnknown berarti kode modul tidak ada di katalog.
var ErrUnknown = errors.New("module: unknown module")

// ErrCore berarti modul core tidak bisa diubah.
var ErrCore = errors.New("module: core is always enabled")

// Auditor mencatat perubahan modul ke audit, dalam transaksi yang sama
// dengan Save; dipasok pemanggil agar paket ini tidak bergantung pada paket
// audit.
type Auditor func(ctx context.Context, schoolID, action string, before, after State) error

// Save menulis status modul id sekolah, menghapus cache replika ini, dan
// mencatat audit "module.<id>" lewat audit (boleh nil di uji). by adalah
// pelaku (mis. "akra-modul (budi)").
func Save(ctx context.Context, q database.DBTX, schoolID string, id ID, st State, by string, audit Auditor) error {
	if id == Core {
		return ErrCore
	}
	if !Known(id) {
		return fmt.Errorf("%w: %q", ErrUnknown, id)
	}
	before, _, err := GetState(ctx, q, schoolID, id)
	if err != nil {
		return err
	}
	opts := st.Options
	if opts == nil {
		opts = map[string]string{}
	}
	raw, err := json.Marshal(opts)
	if err != nil {
		return fmt.Errorf("module: options: %w", err)
	}
	if _, err := q.Exec(ctx, `
		INSERT INTO school_modules (school_id, module, enabled, options, changed_at, changed_by, note)
		VALUES ($1, $2, $3, $4, now(), $5, $6)
		ON CONFLICT (school_id, module) DO UPDATE SET enabled = EXCLUDED.enabled, options = EXCLUDED.options,
			changed_at = now(), changed_by = EXCLUDED.changed_by, note = EXCLUDED.note`,
		schoolID, id, st.Enabled, raw, by, st.Note); err != nil {
		return fmt.Errorf("module: save %s: %w", id, err)
	}
	Forget(schoolID)
	if audit != nil {
		return audit(ctx, schoolID, "module."+string(id), before, st)
	}
	return nil
}

type ctxKey struct{}

// With menyimpan set modul s di ctx.
func With(ctx context.Context, s Set) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

// From mengembalikan set modul request. Bila Middleware tidak terpasang
// (mis. router uji paket), bawaan katalog dikembalikan.
func From(ctx context.Context) Set {
	if s, ok := ctx.Value(ctxKey{}).(Set); ok {
		return s
	}
	return Defaults()
}

// Middleware memuat set modul sekolah sekali per request ke context (lihat
// From). schoolOf mengembalikan sekolah request ("" = lewati, mis. belum
// login); fail dipanggil bila status gagal dibaca.
func Middleware(q database.DBTX, schoolOf func(*http.Request) string, fail func(http.ResponseWriter, *http.Request, error)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := schoolOf(r)
			if id == "" {
				next.ServeHTTP(w, r)
				return
			}
			s, err := Load(r.Context(), q, id)
			if err != nil {
				fail(w, r, err)
				return
			}
			next.ServeHTTP(w, r.WithContext(With(r.Context(), s)))
		})
	}
}

// Enforce mengembalikan middleware yang menolak request ke route modul yang
// tidak aktif untuk sekolah (T164), memakai set modul dari Middleware.
// deny dipanggil dengan modul pertama yang tidak aktif dan harus menjawab
// 404 tanpa data modul itu.
func Enforce(deny func(http.ResponseWriter, *http.Request, Info)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if miss := From(r.Context()).Missing(r.URL.Path); len(miss) > 0 {
				info, _ := Get(miss[0])
				deny(w, r, info)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Inactive adalah pesan halaman staf untuk modul m yang tidak aktif.
func Inactive(m Info) string {
	return "Modul " + m.Name + " tidak aktif untuk sekolah Anda. Hubungi admin TU atau tim AKRA."
}

// Row adalah status tersimpan satu modul (untuk halaman Modul & paket).
type Row struct {
	Enabled   bool
	ChangedAt time.Time
}

// Rows mengembalikan status tersimpan semua modul sekolah tanpa cache.
// Modul tanpa baris memakai bawaan katalog (tidak ada di map).
func Rows(ctx context.Context, q database.DBTX, schoolID string) (map[ID]Row, error) {
	rows, err := q.Query(ctx, `SELECT module, enabled, changed_at FROM school_modules WHERE school_id = $1`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("module: rows: %w", err)
	}
	defer rows.Close()
	out := map[ID]Row{}
	for rows.Next() {
		var id ID
		var r Row
		if err := rows.Scan(&id, &r.Enabled, &r.ChangedAt); err != nil {
			return nil, fmt.Errorf("module: rows: %w", err)
		}
		out[id] = r
	}
	return out, rows.Err()
}

// PackageLabels adalah nama paket untuk tampilan (ADR 0002 bagian 5).
// schools.package kosong berarti paket kustom atau belum diberi label.
var PackageLabels = map[string]string{
	"dasar":    "Paket Dasar",
	"akademik": "Paket Akademik",
	"keuangan": "Paket Keuangan",
	"lengkap":  "Paket Lengkap",
}
