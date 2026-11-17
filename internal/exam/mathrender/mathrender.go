// Package mathrender merender rumus LaTeX menjadi HTML di server dengan
// KaTeX (dijalankan di goja), sesuai ADR 0001: rumus dirender saat soal
// disimpan, sehingga halaman ujian tidak memuat JavaScript rumus.
package mathrender

import (
	"errors"
	"fmt"
	"io/fs"
	"runtime"
	"strings"
	"sync"

	"github.com/dop251/goja"
)

// KatexPath adalah lokasi katex.min.js di FS aset statis (web/static). File yang sama
// disajikan ke editor guru untuk pratinjau, sehingga versi server dan
// browser selalu sama.
const KatexPath = "vendor/katex/katex.min.js"

// MaxTeX membatasi panjang satu rumus.
const MaxTeX = 2000

// Error adalah rumus yang tidak bisa dirender (pesan dari KaTeX).
type Error struct {
	TeX     string
	Message string
}

func (e *Error) Error() string { return "mathrender: " + e.Message }

type renderFunc func(tex string, display bool) (string, error)

// Renderer merender LaTeX. goja.Runtime tidak aman dipakai bersamaan, jadi
// Renderer memegang pool VM (paling banyak GOMAXPROCS).
type Renderer struct {
	src  string
	pool chan renderFunc
	once sync.Once
	n    int
	mu   sync.Mutex
}

// New membaca katex.min.js dari fsys. VM dibuat saat pertama dipakai.
func New(fsys fs.FS) (*Renderer, error) {
	b, err := fs.ReadFile(fsys, KatexPath)
	if err != nil {
		return nil, fmt.Errorf("mathrender: read katex: %w", err)
	}
	size := runtime.GOMAXPROCS(0)
	if size > 4 {
		size = 4
	}
	return &Renderer{src: string(b), pool: make(chan renderFunc, size)}, nil
}

// Opsi keamanan (ADR 0001): trust:false menolak \href/\url berbahaya,
// throwOnError agar rumus salah ditolak saat simpan, maxExpand mencegah
// makro rekursif, maxSize membatasi ukuran.
const setup = `function __akraRender(tex, display) {
	return katex.renderToString(tex, {displayMode: display, throwOnError: true, output: "html",
		trust: false, strict: "ignore", maxExpand: 200, maxSize: 20});
}`

func (r *Renderer) newVM() (renderFunc, error) {
	vm := goja.New()
	if _, err := vm.RunString(r.src); err != nil {
		return nil, fmt.Errorf("mathrender: load katex: %w", err)
	}
	if _, err := vm.RunString(setup); err != nil {
		return nil, fmt.Errorf("mathrender: setup: %w", err)
	}
	var fn renderFunc
	if err := vm.ExportTo(vm.Get("__akraRender"), &fn); err != nil {
		return nil, fmt.Errorf("mathrender: export: %w", err)
	}
	return fn, nil
}

// get mengambil VM dari pool, atau membuat baru bila pool belum penuh.
func (r *Renderer) get() (renderFunc, error) {
	select {
	case fn := <-r.pool:
		return fn, nil
	default:
	}
	r.mu.Lock()
	if r.n < cap(r.pool) {
		r.n++
		r.mu.Unlock()
		fn, err := r.newVM()
		if err != nil {
			r.mu.Lock()
			r.n--
			r.mu.Unlock()
		}
		return fn, err
	}
	r.mu.Unlock()
	return <-r.pool, nil
}

// Render merender satu rumus. Rumus kosong, terlalu panjang, atau tidak
// valid menghasilkan *Error.
func (r *Renderer) Render(tex string, display bool) (string, error) {
	tex = strings.TrimSpace(tex)
	if tex == "" {
		return "", &Error{TeX: tex, Message: "rumus kosong"}
	}
	if len(tex) > MaxTeX {
		return "", &Error{TeX: tex, Message: fmt.Sprintf("rumus lebih dari %d karakter", MaxTeX)}
	}
	if cmd := forbiddenCommand(tex); cmd != "" {
		return "", &Error{TeX: tex, Message: "perintah " + cmd + " tidak diizinkan di rumus"}
	}
	fn, err := r.get()
	if err != nil {
		return "", err
	}
	defer func() { r.pool <- fn }()
	out, err := fn(tex, display)
	if err != nil {
		var ex *goja.Exception
		msg := err.Error()
		if errors.As(err, &ex) {
			msg = ex.Value().String()
		}
		return "", &Error{TeX: tex, Message: cleanMessage(msg)}
	}
	return out, nil
}

// cleanMessage merapikan pesan KaTeX ("ParseError: KaTeX parse error: ...").
func cleanMessage(m string) string {
	m = strings.TrimPrefix(m, "ParseError: ")
	m = strings.TrimPrefix(m, "KaTeX parse error: ")
	if i := strings.Index(m, " at "); i > 0 && len(m) > 120 {
		m = m[:i]
	}
	return m
}

// forbidden adalah perintah KaTeX yang membuat tautan, gambar, atau atribut
// HTML. Dengan trust:false KaTeX tidak membuatnya, tetapi rumus seperti ini
// tetap ditolak agar guru tahu rumusnya tidak akan tampil.
var forbidden = []string{`\href`, `\url`, `\includegraphics`, `\htmlClass`, `\htmlId`, `\htmlStyle`, `\htmlData`}

func forbiddenCommand(tex string) string {
	for _, c := range forbidden {
		if strings.Contains(tex, c) {
			return c
		}
	}
	return ""
}
