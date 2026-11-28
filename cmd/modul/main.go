// Command akra-modul adalah pengelola modul dan paket per sekolah untuk tim
// implementator AKRA (T125, T163, T168; ADR 0002). Sekolah tidak menyalakan
// modul sendiri. Semua perubahan lewat module.Admin: ketergantungan dan
// penjaga diperiksa, catatan wajib, dan setiap perubahan tercatat di audit.
//
// Contoh:
//
//	akra-modul -school sdharapan                                   # status satu sekolah
//	akra-modul -school a,b -module billing -on -note "kontrak 2027"
//	akra-modul -school a -module exam -on -option release=r2 -note "pilot ulangan"
//	akra-modul -school a -module billing -off -force -note "SPP dipindah ke yayasan"
//	akra-modul -file gelombang.txt -package lengkap -note "gelombang 2"
//	akra-modul -list                                               # sekolah × modul dan paket
//	akra-modul -list -module exam                                  # satu modul, dengan pilihan
//
// Bentuk lama dari T125/T155 tetap diterima: -exam on|off sama dengan
// -module exam -on|-off, dan -release r sama dengan -option release=r.
//
// Gelombang (T155): satu perintah adalah satu transaksi; bila satu sekolah
// gagal (kode salah, penjaga menolak), tidak ada yang berubah. -file berisi
// satu kode sekolah per baris; baris kosong dan baris diawali # diabaikan.
//
// Koneksi hanya dari environment DATABASE_URL. Pengguna sekolah melihat
// perubahan tanpa login ulang, paling lama 30 detik per replika.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
	"akra/internal/platform/module"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	if err := run(context.Background(), os.Args[1:], os.Stdout); err != nil {
		logger.Error("akra-modul failed", "error", err.Error())
		os.Exit(1)
	}
}

// options adalah perintah yang sudah diurai.
type options struct {
	codes   []string
	list    bool
	module  module.ID
	on, off bool
	opts    map[string]string
	pkg     string
	note    string
	force   bool
}

// optionFlag mengumpulkan -option kunci=nilai (boleh berulang).
type optionFlag map[string]string

func (o optionFlag) String() string { return fmt.Sprint(map[string]string(o)) }

func (o optionFlag) Set(v string) error {
	k, val, ok := strings.Cut(v, "=")
	if !ok || strings.TrimSpace(k) == "" {
		return fmt.Errorf("-option must be key=value, got %q", v)
	}
	o[strings.TrimSpace(k)] = strings.TrimSpace(val)
	return nil
}

func parse(args []string) (options, error) {
	fs := flag.NewFlagSet("akra-modul", flag.ContinueOnError)
	code := fs.String("school", "", "kode sekolah; beberapa dipisah koma")
	file := fs.String("file", "", "berkas berisi kode sekolah, satu per baris")
	list := fs.Bool("list", false, "tampilkan sekolah × modul dan paket (dengan -module: satu modul)")
	mod := fs.String("module", "", "kode modul: "+moduleCodes())
	on := fs.Bool("on", false, "aktifkan -module")
	off := fs.Bool("off", false, "nonaktifkan -module")
	opts := optionFlag{}
	fs.Var(opts, "option", "pilihan modul kunci=nilai, mis. release=r2 (boleh berulang)")
	pkg := fs.String("package", "", "terapkan paket: "+packageCodes())
	note := fs.String("note", "", "catatan perubahan (wajib untuk setiap perubahan)")
	force := fs.Bool("force", false, "lewati peringatan (mis. tagihan belum lunas saat keuangan dimatikan)")
	examFlag := fs.String("exam", "", "bentuk lama: on atau off (= -module exam -on/-off)")
	release := fs.String("release", "", "bentuk lama: tahap ujian r1, r2, r3 (= -option release=...)")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	o := options{list: *list, module: module.ID(strings.TrimSpace(*mod)), on: *on, off: *off, opts: opts,
		pkg: strings.TrimSpace(*pkg), note: strings.TrimSpace(*note), force: *force}
	switch *examFlag {
	case "":
	case "on", "off":
		o.module, o.on, o.off = module.Exam, *examFlag == "on", *examFlag == "off"
	default:
		return options{}, fmt.Errorf("-exam must be on or off, got %q", *examFlag)
	}
	if *release != "" {
		o.module = module.Exam
		o.opts["release"] = *release
	}
	var err error
	if o.codes, err = schoolCodes(*code, *file); err != nil {
		return options{}, err
	}
	switch {
	case o.list:
		if o.module != "" && !module.Known(o.module) {
			return options{}, fmt.Errorf("unknown module %q (modules: %s)", o.module, moduleCodes())
		}
		return o, nil
	case len(o.codes) == 0:
		return options{}, errors.New("-school, -file, or -list is required")
	case o.module != "" && o.pkg != "":
		return options{}, errors.New("use either -module or -package, not both")
	case o.on && o.off:
		return options{}, errors.New("use either -on or -off, not both")
	case o.module != "" && !module.Known(o.module):
		return options{}, fmt.Errorf("unknown module %q (modules: %s)", o.module, moduleCodes())
	case o.module != "" && !o.on && !o.off && len(o.opts) == 0:
		return options{}, errors.New("-module needs -on, -off, or -option")
	case (o.on || o.off || len(o.opts) > 0) && o.module == "":
		return options{}, errors.New("-on, -off, and -option need -module")
	case o.pkg != "":
		if _, ok := module.GetPackage(o.pkg); !ok {
			return options{}, fmt.Errorf("unknown package %q (packages: %s)", o.pkg, packageCodes())
		}
	}
	if (o.module != "" || o.pkg != "") && o.note == "" {
		return options{}, errors.New("-note is required for every change")
	}
	return o, nil
}

func run(ctx context.Context, args []string, out io.Writer) error {
	o, err := parse(args)
	if err != nil {
		return err
	}
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("environment variable DATABASE_URL is required")
	}
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		return err
	}
	defer pool.Close()
	if o.list {
		return listSchools(ctx, pool, o.module, out)
	}
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		for _, c := range o.codes {
			if err := apply(ctx, tx, c, o, out); err != nil {
				return fmt.Errorf("%s: %w", c, err)
			}
		}
		return nil
	})
}

// operator adalah pelaku perubahan di school_modules.changed_by dan audit.
func operator() string {
	if u := os.Getenv("USER"); u != "" {
		return "akra-modul (" + u + ")"
	}
	return "akra-modul"
}

// apply menerapkan perintah o ke satu sekolah, atau hanya menampilkan
// statusnya bila tidak ada perubahan yang diminta.
func apply(ctx context.Context, tx pgx.Tx, code string, o options, out io.Writer) error {
	var schoolID, name, pkg string
	err := tx.QueryRow(ctx, `SELECT id, name, package FROM schools WHERE code = $1`, strings.ToLower(strings.TrimSpace(code))).Scan(&schoolID, &name, &pkg)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("school %q not found", code)
	}
	if err != nil {
		return err
	}
	admin := module.Admin{By: operator()}
	var res module.Result
	switch {
	case o.pkg != "":
		res, err = admin.ApplyPackage(ctx, tx, schoolID, o.pkg, o.note, o.force)
		pkg = o.pkg
	case o.module != "":
		on := o.on
		if !o.on && !o.off { // hanya pilihan: status tetap
			cur, err := module.Load(ctx, tx, schoolID)
			if err != nil {
				return err
			}
			on = cur.Has(o.module)
		}
		res, err = admin.Set(ctx, tx, schoolID, o.module, on, o.opts, o.note, o.force)
	}
	if err != nil {
		return err
	}
	for _, w := range res.Warnings {
		if _, err := fmt.Fprintf(out, "peringatan: %s\n", w); err != nil {
			return err
		}
	}
	set, err := module.Load(ctx, tx, schoolID)
	if err != nil {
		return err
	}
	label := "tanpa paket"
	if p, ok := module.GetPackage(pkg); ok {
		label = p.Name
	}
	changed := "tidak ada perubahan"
	if len(res.Changed) > 0 {
		var ids []string
		for _, id := range res.Changed {
			ids = append(ids, string(id))
		}
		changed = "diubah: " + strings.Join(ids, ", ")
	}
	if o.module == "" && o.pkg == "" {
		changed = "status"
	}
	_, err = fmt.Fprintf(out, "%s (%s) · %s · %s\n  %s\n", name, code, label, changed, describe(set))
	return err
}

// describe menulis modul aktif sekolah, dengan pilihan ujian.
func describe(s module.Set) string {
	var parts []string
	for _, m := range module.Catalog {
		if m.ID == module.Core || !s.Has(m.ID) {
			continue
		}
		p := string(m.ID)
		if rel := s.Option(m.ID, "release"); rel != "" {
			p += " (" + rel + ")"
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return "modul aktif: hanya inti"
	}
	return "modul aktif: " + strings.Join(parts, ", ")
}

// listSchools menulis sekolah × modul dan paket, atau satu modul (dengan
// pilihan dan tanggal perubahan terakhir) bila only diisi.
func listSchools(ctx context.Context, q database.DBTX, only module.ID, out io.Writer) error {
	rows, err := q.Query(ctx, `SELECT s.id, s.code, s.name, s.package,
			(SELECT max(created_at) FROM audit_logs a WHERE a.school_id = s.id AND a.action = 'module.' || $1::text)
		FROM schools s ORDER BY s.code`, string(only))
	if err != nil {
		return err
	}
	type school struct {
		id, code, name, pkg string
		changed             *time.Time
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (school, error) {
		var s school
		return s, r.Scan(&s.id, &s.code, &s.name, &s.pkg, &s.changed)
	})
	if err != nil {
		return err
	}
	var mods []module.Info
	for _, m := range module.Catalog {
		if m.ID != module.Core && (only == "" || m.ID == only) {
			mods = append(mods, m)
		}
	}
	if only == "" {
		head := fmt.Sprintf("%-24s %-9s", "sekolah", "paket")
		for _, m := range mods {
			head += " " + short(m.ID)
		}
		if _, err := fmt.Fprintln(out, head+"  nama"); err != nil {
			return err
		}
	}
	active := 0
	for _, s := range list {
		set, err := module.Load(ctx, q, s.id)
		if err != nil {
			return err
		}
		pkg := s.pkg
		if pkg == "" {
			pkg = "-"
		}
		if only != "" {
			state := "nonaktif"
			if set.Has(only) {
				state = "aktif"
				active++
			}
			opt := ""
			if rel := set.Option(only, "release"); rel != "" {
				opt = " " + rel
			}
			since := ""
			if s.changed != nil {
				since = " · diubah " + s.changed.Format("2006-01-02")
			}
			if _, err := fmt.Fprintf(out, "%-24s %-8s%s %s%s\n", s.code, state, opt, s.name, since); err != nil {
				return err
			}
			continue
		}
		line := fmt.Sprintf("%-24s %-9s", s.code, pkg)
		for _, m := range mods {
			mark := "·"
			if set.Has(m.ID) {
				mark = "✓"
			}
			line += " " + fmt.Sprintf("%-*s", len(short(m.ID)), mark)
		}
		if _, err := fmt.Fprintln(out, line+"  "+s.name); err != nil {
			return err
		}
	}
	if only != "" {
		_, err = fmt.Fprintf(out, "%d sekolah dengan modul %s aktif\n", active, only)
		return err
	}
	_, err = fmt.Fprintf(out, "%d sekolah\n", len(list))
	return err
}

// short adalah singkatan kolom modul di -list.
func short(id module.ID) string {
	s := string(id)
	if len(s) > 5 {
		s = s[:5]
	}
	return s
}

func moduleCodes() string {
	var c []string
	for _, m := range module.Catalog {
		if m.ID != module.Core {
			c = append(c, string(m.ID))
		}
	}
	return strings.Join(c, ", ")
}

func packageCodes() string {
	var c []string
	for _, p := range module.Packages {
		c = append(c, p.Code)
	}
	return strings.Join(c, ", ")
}

// schoolCodes menggabungkan kode dari -school (dipisah koma) dan -file,
// tanpa duplikat.
func schoolCodes(flagCodes, file string) ([]string, error) {
	raw := strings.Split(flagCodes, ",")
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("read -file: %w", err)
		}
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				raw = append(raw, l)
			}
		}
	}
	var out []string
	seen := map[string]bool{}
	for _, c := range raw {
		c = strings.ToLower(strings.TrimSpace(c))
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out, nil
}
