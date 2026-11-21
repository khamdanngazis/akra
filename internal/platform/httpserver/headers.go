package httpserver

import "net/http"

// SecurityHeaders memasang header keamanan dasar di semua respons (T153):
//
//   - X-Content-Type-Options: nosniff, agar berkas unggahan tidak ditafsir
//     ulang browser sebagai HTML/skrip;
//   - X-Frame-Options dan CSP frame-ancestors: halaman AKRA tidak boleh
//     dibingkai situs lain (clickjacking pada tombol simpan/kumpulkan);
//   - Referrer-Policy: URL internal tidak bocor ke situs luar; handler yang
//     butuh lebih ketat (no-referrer) menimpanya;
//   - Strict-Transport-Security hanya bila hsts (di luar development).
func SecurityHeaders(hsts bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Content-Security-Policy", "frame-ancestors 'none'")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if hsts {
				h.Set("Strict-Transport-Security", "max-age=31536000")
			}
			next.ServeHTTP(w, r)
		})
	}
}
