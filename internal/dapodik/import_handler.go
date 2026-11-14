package dapodik

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"strings"

	"akra/internal/auth"
	"akra/internal/importer"
	"akra/internal/school"
)

const sessionImport = "dapodik_import" // "<school_id>|<token>"

type importView struct {
	User   auth.Identity
	Plan   *Plan
	Result *ApplyResult
	Flash  string
	Error  string
}

func importKey(schoolID, token string) string {
	return "schools/" + schoolID + "/dapodik-import/" + token + ".enc"
}

// importForm menampilkan form unggah ekspor Daftar PD. Peran: admin_tu.
func (h *Handler) importForm(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	h.render.Render(w, r, http.StatusOK, "dapodik/import", importView{User: id, Flash: auth.PopFlash(r.Context(), h.sessions)})
}

// importPreview membaca file, menyimpannya terenkripsi sementara, lalu
// menampilkan rencana impor. Peran: admin_tu.
func (h *Handler) importPreview(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, importer.MaxFileSize+64<<10)
	file, _, err := r.FormFile("file")
	if err != nil {
		h.render.Render(w, r, http.StatusBadRequest, "dapodik/import", importView{User: id, Error: "Pilih file Excel ekspor Daftar Peserta Didik dari Dapodik."})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, importer.MaxFileSize+1))
	if err != nil {
		h.serverError(w, r, "read import", err)
		return
	}
	plan, err := PlanPD(r.Context(), h.db, h.keys, id.SchoolID, bytes.NewReader(data))
	if msg := importErrorText(err); msg != "" {
		h.render.Render(w, r, http.StatusBadRequest, "dapodik/import", importView{User: id, Error: msg})
		return
	}
	if err != nil {
		h.serverError(w, r, "plan import", err)
		return
	}
	var tb [16]byte
	if _, err := rand.Read(tb[:]); err != nil {
		h.serverError(w, r, "import token", err)
		return
	}
	token := hex.EncodeToString(tb[:])
	ct, err := h.keys.Encrypt(string(data), "dapodik-import:"+token)
	if err != nil {
		h.serverError(w, r, "encrypt import", err)
		return
	}
	if err := h.store.Put(r.Context(), importKey(id.SchoolID, token), bytes.NewReader(ct), int64(len(ct)), "application/octet-stream"); err != nil {
		h.serverError(w, r, "store import", err)
		return
	}
	h.sessions.Put(r.Context(), sessionImport, id.SchoolID+"|"+token)
	h.render.Render(w, r, http.StatusOK, "dapodik/import", importView{User: id, Plan: &plan})
}

// importApply menerapkan rencana dari file yang tersimpan. Peran: admin_tu.
func (h *Handler) importApply(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := r.ParseForm(); err != nil {
		http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
		return
	}
	schoolID, token, _ := strings.Cut(h.sessions.GetString(r.Context(), sessionImport), "|")
	if schoolID != id.SchoolID || token == "" {
		h.render.Render(w, r, http.StatusBadRequest, "dapodik/import", importView{User: id, Error: "Sesi impor sudah berakhir. Unggah file lagi."})
		return
	}
	key := importKey(id.SchoolID, token)
	body, err := h.store.Get(r.Context(), key)
	if err != nil {
		h.render.Render(w, r, http.StatusBadRequest, "dapodik/import", importView{User: id, Error: "File impor tidak ditemukan. Unggah file lagi."})
		return
	}
	ct, err := io.ReadAll(body)
	body.Close()
	if err != nil {
		h.serverError(w, r, "read stored import", err)
		return
	}
	data, err := h.keys.Decrypt(ct, "dapodik-import:"+token)
	if err != nil {
		h.serverError(w, r, "decrypt import", err)
		return
	}
	// Rencana disusun ulang dari data terbaru agar perubahan sejak pratinjau
	// tidak tertimpa diam-diam.
	plan, err := PlanPD(r.Context(), h.db, h.keys, id.SchoolID, strings.NewReader(data))
	if err != nil {
		h.serverError(w, r, "plan import", err)
		return
	}
	overwrite := map[string]bool{}
	for _, v := range r.PostForm["overwrite"] {
		overwrite[v] = true
	}
	actor := school.Actor{SchoolID: id.SchoolID, UserID: id.UserID, IP: auth.ClientIP(r)}
	res, err := ApplyPD(r.Context(), h.db, h.keys, actor, plan, overwrite)
	if err != nil {
		h.serverError(w, r, "apply import", err)
		return
	}
	_ = h.store.Delete(r.Context(), key)
	h.sessions.Remove(r.Context(), sessionImport)
	h.render.Render(w, r, http.StatusOK, "dapodik/import", importView{User: id, Result: &res})
}

func importErrorText(err error) string {
	switch {
	case err == nil:
		return ""
	case errors.Is(err, ErrNotPDExport):
		return "File ini bukan ekspor Daftar Peserta Didik dari Dapodik (kolom NIPD dan Nama tidak ditemukan)."
	case errors.Is(err, importer.ErrNotXLSX), errors.Is(err, importer.ErrFileEmpty):
		return "File harus berupa Excel .xlsx."
	case errors.Is(err, importer.ErrFileTooLarge):
		return "Ukuran file maksimal 5 MB."
	case errors.Is(err, importer.ErrNoData), errors.Is(err, importer.ErrNoHeader):
		return "File tidak berisi data siswa."
	case errors.Is(err, importer.ErrTooManyRows):
		return "File terlalu banyak baris (maksimal 5.000 siswa)."
	}
	return ""
}
