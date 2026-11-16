// katexbench mengukur render KaTeX di Go (goja) untuk spike T123.
//
//	go run ./katexbench -katex path/katex.min.js
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/dop251/goja"
)

var formulas = []string{
	`\frac{a+b}{c}`, `x^2 + 2x + 1 = 0`, `\sqrt{b^2-4ac}`, `x = \frac{-b \pm \sqrt{b^2-4ac}}{2a}`,
	`\begin{pmatrix} 1 & 2 \\ 3 & 4 \end{pmatrix}`, `\int_0^1 x^2\,dx`, `\sum_{i=1}^{n} i = \frac{n(n+1)}{2}`,
	`\alpha + \beta = 90^\circ`, `\log_2 8 = 3`, `\lim_{x\to 0}\frac{\sin x}{x}=1`, `H_2O`, `\overline{AB} \parallel \overline{CD}`,
}

type renderer func(string) (string, error)

func newVM(src string) (renderer, time.Duration) {
	t0 := time.Now()
	vm := goja.New()
	if _, err := vm.RunString(src); err != nil {
		panic(err)
	}
	if _, err := vm.RunString(`function r(s){return katex.renderToString(s,{throwOnError:true,output:"html",trust:false,strict:"ignore",maxExpand:200,maxSize:20})}`); err != nil {
		panic(err)
	}
	var r renderer
	if err := vm.ExportTo(vm.Get("r"), &r); err != nil {
		panic(err)
	}
	return r, time.Since(t0)
}

func main() {
	path := flag.String("katex", "katex.min.js", "path katex.min.js")
	flag.Parse()
	src, err := os.ReadFile(*path)
	if err != nil {
		panic(err)
	}
	r, initDur := newVM(string(src))
	fmt.Printf("init VM + parse katex.min.js: %v\n", initDur)

	for _, f := range formulas { // pemanasan
		if _, err := r(f); err != nil {
			panic(err)
		}
	}
	var durs []time.Duration
	size := 0
	for i := 0; i < 50; i++ {
		for _, f := range formulas {
			t0 := time.Now()
			h, _ := r(f)
			durs = append(durs, time.Since(t0))
			size += len(h)
		}
	}
	sort.Slice(durs, func(i, j int) bool { return durs[i] < durs[j] })
	var sum time.Duration
	for _, d := range durs {
		sum += d
	}
	fmt.Printf("1 VM: n=%d mean=%v p50=%v p95=%v, mean HTML=%d B\n",
		len(durs), sum/time.Duration(len(durs)), durs[len(durs)/2], durs[len(durs)*95/100], size/len(durs))

	// Pool VM paralel (goja.Runtime tidak aman dipakai bersamaan).
	workers := runtime.NumCPU()
	pool := make([]renderer, workers)
	for i := range pool {
		pool[i], _ = newVM(string(src))
	}
	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	fmt.Printf("pool %d VM: heap %d MB\n", workers, m.HeapAlloc>>20)
	const n = 4000
	var wg sync.WaitGroup
	t0 := time.Now()
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(r renderer) {
			defer wg.Done()
			for i := 0; i < n/workers; i++ {
				r(formulas[i%len(formulas)])
			}
		}(pool[w])
	}
	wg.Wait()
	el := time.Since(t0)
	fmt.Printf("pool %d VM: %d render in %v = %.0f render/s\n", workers, n, el, float64(n)/el.Seconds())

	// Masukan berbahaya / salah.
	for _, bad := range []string{`\frac{a`, `\href{javascript:alert(1)}{x}`, `\def\a{\a}\a`, `<script>`} {
		h, err := r(bad)
		fmt.Printf("input %q: err=%v html=%dB\n", bad, err != nil, len(h))
	}
}
