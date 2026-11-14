package admission

import (
	"encoding/json"
	"net/http"
	"strings"
)

// Unggah dokumen lewat fetch/XHR (T108) memakai route yang sama dengan form
// biasa; bedanya permintaan mengirim "Accept: application/json" dan
// menerima JSON, bukan redirect atau halaman.

// uploadResult adalah respons JSON unggah dokumen.
type uploadResult struct {
	OK      bool   `json:"ok"`
	Message string `json:"message"`
}

// wantsJSON melaporkan apakah klien meminta respons JSON.
func wantsJSON(r *http.Request) bool {
	return strings.Contains(r.Header.Get("Accept"), "application/json")
}

// writeUploadJSON menulis hasil unggah sebagai JSON.
func writeUploadJSON(w http.ResponseWriter, status int, ok bool, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(uploadResult{OK: ok, Message: msg})
}

// Pesan unggah.
const (
	msgPickFile    = "Pilih file yang akan diunggah (maksimal 2 MB)."
	msgUploadError = "Terjadi kesalahan di server. Coba lagi beberapa saat lagi."
)
