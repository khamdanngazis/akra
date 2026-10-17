package httpserver

import "net/http"

// MaxRequestBody adalah batas ukuran body semua request. Dipasang sebelum
// middleware CSRF karena nosurf mem-parse seluruh form multipart (untuk
// mencari token) sebelum handler sempat memasang batasnya sendiri. Handler
// tetap memasang batas yang lebih kecil sesuai kebutuhannya.
const MaxRequestBody = 8 << 20

// LimitBody membatasi body request maksimal n byte. Body yang lebih besar
// membuat pembacaan gagal (http.MaxBytesError).
func LimitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil && r.Body != http.NoBody {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}
