package subnav

import (
	"context"
	"testing"
)

func TestMark(t *testing.T) {
	items := []Item{
		{Label: "Ringkasan", Path: "/ujian", Exact: true},
		{Label: "Bank soal", Path: "/ujian/bank/", Also: []string{"/ujian/soal", "/ujian/tp"}},
		{Label: "Ujian", Path: "/ujian/paket/"},
		{Label: "Sesi saya", Path: "/ujian/sesi-saya", Also: []string{"/ujian/sesi"}},
	}
	for path, want := range map[string]string{
		"/ujian": "Ringkasan", "/ujian/": "Ringkasan", "/ujian/bank/": "Bank soal", "/ujian/bank/1": "Bank soal",
		"/ujian/soal/9/ubah": "Bank soal", "/ujian/tp/": "Bank soal", "/ujian/paket/1/hasil": "Ujian",
		"/ujian/sesi/1/awasi": "Sesi saya", "/ujian/sesi-saya": "Sesi saya", "/ujian/lain": "",
	} {
		got := ""
		for _, it := range Mark(items, path) {
			if it.Active {
				if got != "" {
					t.Errorf("%s: two active items", path)
				}
				got = it.Label
			}
		}
		if got != want {
			t.Errorf("Mark(%q) = %q, want %q", path, got, want)
		}
	}
	if From(context.Background()) != nil {
		t.Error("empty context has subnav")
	}
	if got := From(With(context.Background(), items)); len(got) != len(items) {
		t.Errorf("From = %v", got)
	}
}
