package wilayah

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"

	"akra/internal/platform/database"
)

func TestKode(t *testing.T) {
	for _, k := range []string{"32", "32.73", "32.73.04", "32.73.04.1001"} {
		if !ValidKode(k) {
			t.Errorf("ValidKode(%q) = false", k)
		}
	}
	for _, k := range []string{"", "3", "32.7", "32.73.04.100", "32.73.04.1001.1", "32-73"} {
		if ValidKode(k) {
			t.Errorf("ValidKode(%q) = true", k)
		}
	}
	if !ValidDesaKode("32.73.04.1001") || ValidDesaKode("32.73.04") {
		t.Error("ValidDesaKode")
	}
	if IndukOf("32.73.04.1001") != "32.73.04" || IndukOf("32") != "" || TingkatOf("32.73") != Kabupaten {
		t.Error("IndukOf/TingkatOf")
	}
	if normName("Kec. Sukasari") != "sukasari" || normName("  Desa  Cibiru  Wetan ") != "cibiru wetan" {
		t.Errorf("normName = %q", normName("Kec. Sukasari"))
	}
}

func TestEmbeddedData(t *testing.T) {
	recs, err := readData()
	if err != nil {
		t.Fatal(err)
	}
	count := map[int]int{}
	withPos := 0
	for _, r := range recs {
		count[TingkatOf(r.kode)]++
		if TingkatOf(r.kode) == Desa && r.kodePos != "" {
			withPos++
		}
		if TingkatOf(r.kode) != Desa && r.kodePos != "" {
			t.Fatalf("kode pos on non-village %s", r.kode)
		}
		if (TingkatOf(r.kode) <= Kabupaten) != (r.lat != "") {
			t.Fatalf("coordinate presence wrong for %s", r.kode)
		}
	}
	if count[Provinsi] != 38 || count[Kabupaten] < 500 || count[Kecamatan] < 7000 || count[Desa] < 80000 {
		t.Errorf("counts = %v", count)
	}
	if withPos < count[Desa]*95/100 {
		t.Errorf("villages with postal code = %d of %d", withPos, count[Desa])
	}
	if DataVersion() == "" || DataVersion() != DataVersion() {
		t.Error("DataVersion")
	}
}

func testDB(t *testing.T) pgx.Tx {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := database.Connect(ctx, dsn, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	// Mulai dari tabel kosong di dalam transaksi uji.
	if _, err := tx.Exec(ctx, `DELETE FROM wilayah; DELETE FROM wilayah_meta`); err != nil {
		t.Fatal(err)
	}
	return tx
}

func TestLoadAndQuery(t *testing.T) {
	tx := testDB(t)
	ctx := context.Background()
	loaded, err := EnsureLoaded(ctx, tx)
	if err != nil || !loaded {
		t.Fatalf("first load: %v %v", loaded, err)
	}
	var n int
	var versi string
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM wilayah`).Scan(&n)
	_ = tx.QueryRow(ctx, `SELECT versi FROM wilayah_meta`).Scan(&versi)
	if n < 90000 || versi != DataVersion() {
		t.Fatalf("rows = %d, versi = %q", n, versi)
	}
	if loaded, err := EnsureLoaded(ctx, tx); err != nil || loaded {
		t.Errorf("second load: %v %v", loaded, err)
	}

	prov, err := Children(ctx, tx, "")
	if err != nil || len(prov) != 38 {
		t.Fatalf("provinces = %d, %v", len(prov), err)
	}
	kab, _ := Children(ctx, tx, "32")
	for _, k := range kab {
		if k.Kode == "32.73" && (k.Lat != "-6.910655" || k.Lng != "107.609869") {
			t.Errorf("Kota Bandung point = %q, %q", k.Lat, k.Lng)
		}
	}
	kec, _ := Children(ctx, tx, "32.73")
	found := false
	for _, k := range kec {
		found = found || (k.Kode == "32.73.04" && k.Nama == "Bojongloa Kaler")
	}
	if !found {
		t.Errorf("Kota Bandung districts = %+v", kec)
	}
	desa, _ := Children(ctx, tx, "32.73.04")
	if len(desa) == 0 || desa[0].KodePos == "" || desa[0].Lat != "" || kec[0].Lat != "" {
		t.Errorf("villages = %+v", desa)
	}

	a, err := Resolve(ctx, tx, "32.73.04.1001")
	if err != nil || a.Desa != "Kopo" || a.Kecamatan != "Bojongloa Kaler" || a.Kabupaten != "Kota Bandung" || a.Provinsi != "Jawa Barat" || a.KodePos != "40233" {
		t.Errorf("resolve = %+v, %v", a, err)
	}
	if _, err := Resolve(ctx, tx, "32.73.04.9999"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown village: %v", err)
	}
	if _, err := Resolve(ctx, tx, "32.73.04"); !errors.Is(err, ErrNotFound) {
		t.Errorf("district code as village: %v", err)
	}

	v := map[string]string{"wilayah_kode": "32.73.04.1001", "village": "kopo bdg", "district": "x"}
	if err := Apply(ctx, tx, v); err != nil || v["village"] != "Kopo" || v["district"] != "Bojongloa Kaler" || v["postal_code"] != "40233" {
		t.Errorf("apply = %+v, %v", v, err)
	}
	v = map[string]string{"wilayah_kode": "32.73.04.1001", "postal_code": "40999"}
	_ = Apply(ctx, tx, v)
	if v["postal_code"] != "40999" {
		t.Error("apply overwrote postal code typed by user")
	}
	manual := map[string]string{"village": "Desa Baru Pemekaran", "district": "Sukasari"}
	if err := Apply(ctx, tx, manual); err != nil || manual["village"] != "Desa Baru Pemekaran" {
		t.Errorf("manual address changed: %+v %v", manual, err)
	}

	if kode, ok, err := Match(ctx, tx, "Kec. Bojongloa Kaler", "kopo"); err != nil || !ok || kode != "32.73.04.1001" {
		t.Errorf("match = %q %v %v", kode, ok, err)
	}
	if _, ok, _ := Match(ctx, tx, "", "Kopo"); ok {
		t.Error("match without district")
	}

	// Kode yang hilang dari data baru ditandai tidak aktif, tidak dihapus.
	if _, err := tx.Exec(ctx, `INSERT INTO wilayah (kode, nama, tingkat, induk) VALUES ('32.73.04.9999', 'Desa Lama', 4, '32.73.04')`); err != nil {
		t.Fatal(err)
	}
	if err := Reload(ctx, tx); err != nil {
		t.Fatal(err)
	}
	var aktif bool
	if err := tx.QueryRow(ctx, `SELECT aktif FROM wilayah WHERE kode = '32.73.04.9999'`).Scan(&aktif); err != nil || aktif {
		t.Errorf("removed code active=%v, %v", aktif, err)
	}

	// Endpoint publik.
	r := chi.NewRouter()
	NewHandler(tx, slog.New(slog.NewTextHandler(io.Discard, nil))).Routes(r)
	get := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}
	rec := get("/wilayah")
	var list []Region
	if err := json.Unmarshal(rec.Body.Bytes(), &list); rec.Code != http.StatusOK || err != nil || len(list) != 38 || rec.Header().Get("Cache-Control") == "" {
		t.Errorf("GET /wilayah: %d %d %v", rec.Code, len(list), err)
	}
	rec = get("/wilayah?induk=32.73.04")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) == 0 || list[0].KodePos == "" {
		t.Errorf("villages endpoint: %v %+v", err, list)
	}
	for _, bad := range []string{"/wilayah?induk=abc", "/wilayah?induk=32.73.04.1001"} {
		if rec := get(bad); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: %d", bad, rec.Code)
		}
	}
	if rec := get("/wilayah?induk=99.99"); rec.Code != http.StatusOK || rec.Body.String() != "[]\n" {
		t.Errorf("unknown parent: %d %q", rec.Code, rec.Body.String())
	}
}
