package admission

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"akra/internal/platform/pii"
	"akra/internal/platform/storage"
)

func pngBytes(t *testing.T, marker string) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	// Teks penanda di akhir file (tetap PNG yang valid untuk deteksi isi).
	b.WriteString(marker)
	return b.Bytes()
}

func TestDocuments(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	f := e.form() // dokumen wajib: kk, akta
	p, err := e.svc.Create(ctx, e.actor, f)
	if err != nil {
		t.Fatal(err)
	}
	tr, _ := e.svc.AddTrack(ctx, e.actor, p.ID, TrackForm{Name: "Reguler"})
	if _, err := e.svc.SetStatus(ctx, e.actor, p.ID, StatusOpen); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	store, err := storage.NewLocal(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys := pii.DevKeyring()
	apps := NewApplicantService(e.tx, keys, e.svc)
	docs := NewDocumentService(e.tx, store, keys)
	a, _, err := apps.CreateDraft(ctx, e.schoolA, draftForm(p, tr), "")
	if err != nil {
		t.Fatal(err)
	}
	fill(t, apps, e.schoolA, a.ID)

	// Dokumen wajib belum ada: tidak bisa dikirim.
	var ve *ValidationError
	if _, err := apps.Submit(ctx, e.schoolA, a.ID); !errors.As(err, &ve) || !strings.Contains(ve.Fields["submit"], "dokumen") {
		t.Fatalf("submit without documents: %v", err)
	}

	// Tipe dan ukuran diperiksa dari isi file.
	exe := append([]byte("MZ\x90\x00"), bytes.Repeat([]byte{0}, 100)...)
	if _, err := docs.Upload(ctx, e.schoolA, a.ID, "kk", bytes.NewReader(exe)); !errors.Is(err, ErrDocType) {
		t.Errorf("exe renamed: %v", err)
	}
	if _, err := docs.Upload(ctx, e.schoolA, a.ID, "kk", bytes.NewReader(nil)); !errors.Is(err, ErrDocEmpty) {
		t.Errorf("empty: %v", err)
	}
	big := append(pngBytes(t, ""), bytes.Repeat([]byte{0}, MaxDocumentSize)...)
	if _, err := docs.Upload(ctx, e.schoolA, a.ID, "kk", bytes.NewReader(big)); !errors.Is(err, ErrDocTooLarge) {
		t.Errorf("too large: %v", err)
	}

	secret := "NIK-3201010101100001-DI-KK"
	kk, err := docs.Upload(ctx, e.schoolA, a.ID, "kk", bytes.NewReader(pngBytes(t, secret)))
	if err != nil {
		t.Fatal(err)
	}
	// File di storage terenkripsi.
	raw, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(kk.StorageKey)))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(secret)) || bytes.Contains(raw, []byte("PNG")) {
		t.Error("stored document is not encrypted")
	}
	// Ganti dokumen: objek lama dihapus.
	kk2, err := docs.Upload(ctx, e.schoolA, a.ID, "kk", bytes.NewReader(pngBytes(t, secret+"-2")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(kk.StorageKey))); !os.IsNotExist(err) {
		t.Error("old document object not deleted")
	}
	if _, err := docs.Upload(ctx, e.schoolA, a.ID, "akta", bytes.NewReader(pngBytes(t, "akta"))); err != nil {
		t.Fatal(err)
	}
	list, _ := docs.List(ctx, e.schoolA, a.ID)
	if len(list) != 2 || len(MissingDocs(p, list)) != 0 {
		t.Errorf("documents = %+v", list)
	}

	// Staf membuka: didekripsi dan diaudit. Sekolah lain: tidak ditemukan.
	d, body, err := docs.Open(ctx, e.actor, a.ID, kk2.ID)
	if err != nil || !bytes.HasSuffix(body, []byte(secret+"-2")) || d.ContentType != "image/png" {
		t.Errorf("Open = %+v, %d bytes, %v", d, len(body), err)
	}
	var audits int
	_ = e.tx.QueryRow(ctx, `SELECT count(*) FROM audit_logs WHERE action = $1 AND entity_id = $2`, ActionDocumentView, a.ID).Scan(&audits)
	if audits != 1 {
		t.Errorf("document view audits = %d", audits)
	}
	actorB := e.actor
	actorB.SchoolID = e.schoolB
	if _, _, err := docs.Open(ctx, actorB, a.ID, kk2.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("open from other school: %v", err)
	}
	if _, err := docs.Upload(ctx, e.schoolB, a.ID, "kk", bytes.NewReader(pngBytes(t, "x"))); !errors.Is(err, ErrNotFound) {
		t.Errorf("upload from other school: %v", err)
	}
	// Kunci salah: gagal, tidak mengembalikan isi rusak.
	other, _ := pii.NewKeyring(map[byte][]byte{1: bytes.Repeat([]byte{7}, pii.KeySize)}, bytes.Repeat([]byte{8}, pii.KeySize))
	if _, _, err := NewDocumentService(e.tx, store, other).Open(ctx, e.actor, a.ID, kk2.ID); !errors.Is(err, pii.ErrDecrypt) {
		t.Errorf("wrong key: %v", err)
	}

	if _, err := apps.Submit(ctx, e.schoolA, a.ID); err != nil {
		t.Fatalf("submit with documents: %v", err)
	}
	if _, err := docs.Upload(ctx, e.schoolA, a.ID, "foto", bytes.NewReader(pngBytes(t, "f"))); !errors.Is(err, ErrNotEditable) {
		t.Errorf("upload after submit: %v", err)
	}
}

func TestPublicDocumentUpload(t *testing.T) {
	e := newEnv(t)
	p, tr := e.openPeriod(t)
	if _, err := e.tx.Exec(context.Background(), `UPDATE admission_periods SET required_docs = '{kk,akta}' WHERE id = $1`, p.ID); err != nil {
		t.Fatal(err)
	}
	c := newPublicServer(t, e)
	base := "/s/test-adm-a/daftar"
	start := map[string][]string{"choice": {p.ID + "|" + tr.ID}, "full_name": {"Ani"}, "gender": {"P"}, "birth_date": {"2019-05-17"}, "phone": {"081234567891"}, "consent": {"on"}}
	if code, _, _ := c.post(base+"/", start); code != http.StatusSeeOther {
		t.Fatalf("create draft: %d", code)
	}
	upload := func(docType string, data []byte) (int, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "dok.pdf")
		_, _ = fw.Write(data)
		_ = mw.Close()
		res, err := c.client.Post(c.srv.URL+base+"/dokumen/"+docType, mw.FormDataContentType(), &buf)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	if code, body := upload("kk", []byte("bukan pdf")); code != http.StatusBadRequest || !strings.Contains(body, "JPG/PNG") {
		t.Errorf("text upload: %d", code)
	}
	if code, _ := upload("kk", pngBytes(t, "")); code != http.StatusSeeOther {
		t.Errorf("png upload: %d", code)
	}
	if code, _ := upload("ktp", pngBytes(t, "")); code != http.StatusNotFound {
		t.Errorf("unknown type: %d", code)
	}
	// Unggah lewat XHR (T108): respons JSON, tanpa redirect.
	uploadJSON := func(docType string, data []byte, withFile bool) (int, uploadResult) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		if withFile {
			fw, _ := mw.CreateFormFile("file", "dok.jpg")
			_, _ = fw.Write(data)
		}
		_ = mw.Close()
		req, _ := http.NewRequest(http.MethodPost, c.srv.URL+base+"/dokumen/"+docType, &buf)
		req.Header.Set("Content-Type", mw.FormDataContentType())
		req.Header.Set("Accept", "application/json")
		res, err := c.client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out uploadResult
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatalf("json response for %s: %v", docType, err)
		}
		return res.StatusCode, out
	}
	if code, out := uploadJSON("foto", pngBytes(t, ""), true); code != http.StatusOK || !out.OK || !strings.Contains(out.Message, "Pas foto") {
		t.Errorf("json upload: %d %+v", code, out)
	}
	if code, out := uploadJSON("foto", []byte("bukan gambar"), true); code != http.StatusBadRequest || out.OK || !strings.Contains(out.Message, "JPG/PNG") {
		t.Errorf("json bad type: %d %+v", code, out)
	}
	if code, out := uploadJSON("foto", nil, false); code != http.StatusBadRequest || out.OK || out.Message == "" {
		t.Errorf("json without file: %d %+v", code, out)
	}
	if _, body := c.get(base + "/isi/dokumen"); !strings.Contains(body, "js/admission-upload.js") || !strings.Contains(body, `data-url="`+base+`/dokumen/kk"`) || strings.Contains(body, "Surat keterangan pindah") {
		t.Error("document step without auto-upload component")
	}
	// Unggah langsung dari halaman periksa kembali ke halaman periksa (T107).
	_, review := c.get(base + "/isi/kirim")
	if !strings.Contains(review, `action="`+base+`/dokumen/akta?kembali=kirim"`) {
		t.Error("review page without inline upload for missing document")
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", "akta.png")
	_, _ = fw.Write(pngBytes(t, ""))
	_ = mw.Close()
	res, err := c.client.Post(c.srv.URL+base+"/dokumen/akta?kembali=kirim", mw.FormDataContentType(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != http.StatusSeeOther || !strings.HasSuffix(res.Header.Get("Location"), "/isi/kirim") {
		t.Errorf("upload from review: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	_, body := c.get(base + "/isi/dokumen")
	if !strings.Contains(body, "Terunggah") {
		t.Error("uploaded badge missing")
	}
}
