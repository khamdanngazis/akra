package module

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"akra/internal/audit"
)

// Pengelolaan modul oleh tim implementator AKRA (T168, keputusan 1 ADR
// 0002). Admin adalah satu-satunya jalan mengubah school_modules dan
// schools.package: dipakai CLI akra-modul sekarang dan UI implementator
// nanti. Setiap perubahan butuh catatan dan tercatat di audit.

// Package adalah templat kumpulan modul (ADR 0002 bagian 5). Konfigurasi
// sekolah tetap daftar modul; paket hanya cara cepat dan label.
type Package struct {
	Code, Name, For string
	Modules         []ID // tanpa core (selalu aktif)
}

// Packages adalah paket templat (keputusan 2 ADR 0002).
var Packages = []Package{
	{Code: "dasar", Name: "Paket Dasar", For: "Sekolah yang mulai digital: data siswa, absensi, komunikasi",
		Modules: []ID{Attendance, Schedule, Announcements, Portal}},
	{Code: "akademik", Name: "Paket Akademik", For: "Operator yang mengurus rapor dan Dapodik",
		Modules: []ID{Attendance, Schedule, Announcements, Portal, Grades, Dapodik}},
	{Code: "keuangan", Name: "Paket Keuangan", For: "Yayasan yang hanya perlu SPP dan tagihan",
		Modules: []ID{Billing, Announcements, Portal}},
	{Code: "lengkap", Name: "Paket Lengkap", For: "Sekolah swasta dengan SPMB dan ujian online",
		Modules: []ID{Attendance, Schedule, Grades, Billing, Admission, Dapodik, Exam, Announcements, Portal}},
}

// DefaultPackage adalah paket sekolah baru.
const DefaultPackage = "dasar"

// GetPackage mengembalikan paket dengan kode code.
func GetPackage(code string) (Package, bool) {
	for _, p := range Packages {
		if p.Code == code {
			return p, true
		}
	}
	return Package{}, false
}

// Pilihan modul yang dikenal beserta nilai yang boleh (kosong = bebas).
var optionValues = map[ID]map[string][]string{
	Exam: {"release": {"r1", "r2", "r3"}},
}

var (
	// ErrNoteRequired: setiap perubahan modul butuh catatan.
	ErrNoteRequired = errors.New("module: a note is required for every change")
	// ErrUnknownPackage: kode paket tidak ada di Packages.
	ErrUnknownPackage = errors.New("module: unknown package")
	// ErrBadOption: pilihan modul tidak dikenal atau nilainya tidak boleh.
	ErrBadOption = errors.New("module: invalid option")
)

// DependencyError: ketergantungan wajib antarmodul tidak terpenuhi.
type DependencyError struct {
	Module, Other ID
	Enabling      bool // true: Module butuh Other; false: Other butuh Module
}

func (e *DependencyError) Error() string {
	if e.Enabling {
		return fmt.Sprintf("module %s needs module %s to be enabled first", e.Module, e.Other)
	}
	return fmt.Sprintf("module %s is required by enabled module %s", e.Module, e.Other)
}

// GuardError: modul sedang dipakai sehingga tidak bisa dimatikan. Forceable
// berarti hanya peringatan yang bisa dilewati dengan force (keuangan).
type GuardError struct {
	Module    ID
	Reason    string
	Forceable bool
}

func (e *GuardError) Error() string {
	if e.Forceable {
		return fmt.Sprintf("module %s: %s (use force to continue)", e.Module, e.Reason)
	}
	return fmt.Sprintf("module %s cannot be disabled: %s", e.Module, e.Reason)
}

// Admin mengubah modul sekolah. By adalah pelaku (mis. "akra-modul
// (budi)"), dicatat di school_modules.changed_by dan audit.
type Admin struct {
	By string
}

// Result adalah hasil satu perubahan: modul yang berubah dan peringatan
// yang dilewati dengan force.
type Result struct {
	Changed  []ID
	Warnings []string
}

// Enable mengaktifkan modul id dengan pilihan opts (digabung dengan pilihan
// yang ada).
func (a Admin) Enable(ctx context.Context, tx pgx.Tx, schoolID string, id ID, opts map[string]string, note string) (Result, error) {
	return a.Set(ctx, tx, schoolID, id, true, opts, note, false)
}

// Disable mematikan modul id. force melewati peringatan (bukan larangan).
func (a Admin) Disable(ctx context.Context, tx pgx.Tx, schoolID string, id ID, note string, force bool) (Result, error) {
	return a.Set(ctx, tx, schoolID, id, false, nil, note, force)
}

// Set mengubah status dan pilihan modul id. Tanpa perubahan nyata, tidak
// ada yang ditulis dan tidak ada audit.
func (a Admin) Set(ctx context.Context, tx pgx.Tx, schoolID string, id ID, on bool, opts map[string]string, note string, force bool) (Result, error) {
	if strings.TrimSpace(note) == "" {
		return Result{}, ErrNoteRequired
	}
	if id == Core {
		return Result{}, ErrCore
	}
	if !Known(id) {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknown, id)
	}
	if err := checkOptions(id, opts); err != nil {
		return Result{}, err
	}
	cur, err := load(ctx, tx, schoolID)
	if err != nil {
		return Result{}, err
	}
	target := cur.clone()
	target.set(id, on, opts)
	return a.apply(ctx, tx, schoolID, cur, target, note, force)
}

// ApplyPackage menerapkan paket code: modul paket dinyalakan, modul di luar
// paket dimatikan (dengan penjaga), dan schools.package diisi. Pilihan
// modul yang sudah ada dipertahankan.
func (a Admin) ApplyPackage(ctx context.Context, tx pgx.Tx, schoolID, code, note string, force bool) (Result, error) {
	if strings.TrimSpace(note) == "" {
		return Result{}, ErrNoteRequired
	}
	p, ok := GetPackage(code)
	if !ok {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownPackage, code)
	}
	cur, err := load(ctx, tx, schoolID)
	if err != nil {
		return Result{}, err
	}
	target := cur.clone()
	for _, m := range Catalog {
		if m.ID != Core {
			target.set(m.ID, slices.Contains(p.Modules, m.ID), nil)
		}
	}
	res, err := a.apply(ctx, tx, schoolID, cur, target, note, force)
	if err != nil {
		return res, err
	}
	var before string
	if err := tx.QueryRow(ctx, `SELECT package FROM schools WHERE id = $1`, schoolID).Scan(&before); err != nil {
		return res, fmt.Errorf("module: school package: %w", err)
	}
	if before != code {
		if _, err := tx.Exec(ctx, `UPDATE schools SET package = $2 WHERE id = $1`, schoolID, code); err != nil {
			return res, fmt.Errorf("module: set package: %w", err)
		}
		if err := audit.Record(ctx, tx, audit.Entry{SchoolID: schoolID, Action: "module.package", EntityType: "school", EntityID: schoolID,
			Before: map[string]any{"package": before}, After: map[string]any{"package": code, "note": note, "by": a.By}}); err != nil {
			return res, err
		}
	}
	return res, nil
}

// apply memeriksa ketergantungan dan penjaga untuk perubahan cur → target,
// lalu menulis setiap modul yang berubah beserta auditnya. Semua
// pemeriksaan selesai sebelum ada yang ditulis.
func (a Admin) apply(ctx context.Context, tx pgx.Tx, schoolID string, cur, target Set, note string, force bool) (Result, error) {
	var res Result
	var changed []ID
	for _, m := range Catalog {
		if m.ID == Core {
			continue
		}
		if cur.Has(m.ID) != target.Has(m.ID) || !maps.Equal(cur.opts[m.ID], target.opts[m.ID]) {
			changed = append(changed, m.ID)
		}
	}
	for _, id := range changed {
		info, _ := Get(id)
		if target.Has(id) {
			for _, r := range info.Requires {
				if !target.Has(r) {
					return Result{}, &DependencyError{Module: id, Other: r, Enabling: true}
				}
			}
			continue
		}
		if !cur.Has(id) {
			continue // tetap mati, hanya pilihan
		}
		for _, other := range Catalog {
			if target.Has(other.ID) && slices.Contains(other.Requires, id) {
				return Result{}, &DependencyError{Module: id, Other: other.ID}
			}
		}
		g, err := guard(ctx, tx, schoolID, id)
		if err != nil {
			return Result{}, err
		}
		if g != nil {
			if !g.Forceable || !force {
				return Result{}, g
			}
			res.Warnings = append(res.Warnings, g.Error())
		}
	}
	for _, id := range changed {
		before, _, err := GetState(ctx, tx, schoolID, id)
		if err != nil {
			return Result{}, err
		}
		after := State{Enabled: target.Has(id), Options: target.opts[id], Note: note}
		if err := save(ctx, tx, schoolID, id, after, a.By); err != nil {
			return Result{}, err
		}
		if err := audit.Record(ctx, tx, audit.Entry{SchoolID: schoolID, Action: "module." + string(id), EntityType: "school", EntityID: schoolID,
			Before: before, After: map[string]any{"enabled": after.Enabled, "options": after.Options, "note": note, "by": a.By}}); err != nil {
			return Result{}, err
		}
		res.Changed = append(res.Changed, id)
	}
	return res, nil
}

// guard memeriksa apakah modul id sedang dipakai sehingga tidak boleh
// dimatikan (nil = boleh).
func guard(ctx context.Context, tx pgx.Tx, schoolID string, id ID) (*GuardError, error) {
	var n int
	var err error
	switch id {
	case Exam:
		err = tx.QueryRow(ctx, `SELECT count(*) FROM exam_sessions
			WHERE school_id = $1 AND cancelled_at IS NULL AND ends_at > now() AND starts_at < now() + interval '24 hours'`, schoolID).Scan(&n)
		if err == nil && n > 0 {
			return &GuardError{Module: id, Reason: fmt.Sprintf("%d exam session(s) running or starting within 24 hours", n)}, nil
		}
	case Admission:
		err = tx.QueryRow(ctx, `SELECT count(*) FROM admission_periods WHERE school_id = $1 AND status = 'buka'`, schoolID).Scan(&n)
		if err == nil && n > 0 {
			return &GuardError{Module: id, Reason: fmt.Sprintf("%d admission period(s) are open", n)}, nil
		}
	case Billing:
		err = tx.QueryRow(ctx, `SELECT count(*) FROM invoices WHERE school_id = $1 AND voided_at IS NULL AND status <> 'lunas'`, schoolID).Scan(&n)
		if err == nil && n > 0 {
			return &GuardError{Module: id, Forceable: true,
				Reason: fmt.Sprintf("%d unpaid invoice(s) stay stored but parents will no longer see them", n)}, nil
		}
	}
	if err != nil {
		return nil, fmt.Errorf("module: guard %s: %w", id, err)
	}
	return nil, nil
}

func checkOptions(id ID, opts map[string]string) error {
	allowed := optionValues[id]
	for k, v := range opts {
		vals, ok := allowed[k]
		if !ok {
			return fmt.Errorf("%w: module %s has no option %q", ErrBadOption, id, k)
		}
		if len(vals) > 0 && !slices.Contains(vals, v) {
			return fmt.Errorf("%w: %s=%q (allowed: %s)", ErrBadOption, k, v, strings.Join(vals, ", "))
		}
	}
	return nil
}

func (s Set) clone() Set {
	c := Set{on: maps.Clone(s.on), opts: map[ID]map[string]string{}}
	if c.on == nil {
		c.on = map[ID]bool{}
	}
	for id, o := range s.opts {
		c.opts[id] = maps.Clone(o)
	}
	return c
}

// set mengubah status modul id; opts digabung dengan pilihan yang ada.
func (s *Set) set(id ID, on bool, opts map[string]string) {
	if on {
		s.on[id] = true
	} else {
		delete(s.on, id)
	}
	if len(opts) > 0 {
		o := maps.Clone(s.opts[id])
		if o == nil {
			o = map[string]string{}
		}
		maps.Copy(o, opts)
		s.opts[id] = o
	}
}
