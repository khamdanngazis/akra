// Package subnav menyimpan sub-navigasi modul (mis. modul ujian, T157) di
// context request. Modul mengisinya di middleware; layout merendernya di
// bawah header halaman staf.
package subnav

import (
	"context"
	"strings"
)

// Item adalah satu tautan sub-navigasi.
type Item struct {
	Label string
	Path  string
	// Also adalah awalan path lain yang menyorot item ini.
	Also []string
	// Exact: item hanya aktif di Path persis (mis. ringkasan modul).
	Exact bool
	// End: diletakkan di ujung kanan (mis. Bantuan).
	End    bool
	Active bool
}

type key struct{}

// With menyimpan items di ctx.
func With(ctx context.Context, items []Item) context.Context {
	return context.WithValue(ctx, key{}, items)
}

// From mengembalikan sub-navigasi request, atau nil.
func From(ctx context.Context) []Item {
	items, _ := ctx.Value(key{}).([]Item)
	return items
}

// Mark menandai item aktif untuk path: sama persis, atau path berada di
// bawah Path/Also (awalan terpanjang menang).
func Mark(items []Item, path string) []Item {
	if len(path) > 1 {
		path = strings.TrimSuffix(path, "/")
	}
	best, bestLen := -1, 0
	for i, it := range items {
		if strings.TrimSuffix(it.Path, "/") == path {
			best = i
			break
		}
		if it.Exact {
			continue
		}
		for _, p := range append([]string{it.Path}, it.Also...) {
			p = strings.TrimSuffix(p, "/")
			if (path == p || strings.HasPrefix(path, p+"/")) && len(p) > bestLen {
				best, bestLen = i, len(p)
			}
		}
	}
	out := make([]Item, len(items))
	for i, it := range items {
		it.Active = i == best
		out[i] = it
	}
	return out
}
