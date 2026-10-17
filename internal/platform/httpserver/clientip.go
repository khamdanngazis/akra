package httpserver

import (
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ClientIP mengembalikan middleware yang mengganti r.RemoteAddr dengan IP
// dari header yang diberikan proxy, misalnya "X-Real-IP" di Railway.
//
// Hanya aktifkan jika SEMUA traffic melewati proxy yang menimpa header itu;
// jika tidak, klien bisa memalsukan IP-nya dan melewati lockout per IP.
// header kosong berarti middleware tidak melakukan apa pun. Untuk
// X-Forwarded-For dipakai alamat paling kanan, yaitu yang ditambahkan proxy
// terakhir.
func ClientIP(header string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if header == "" {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if ip, ok := ipFromHeader(r.Header.Get(header)); ok {
				r2 := r.Clone(r.Context())
				r2.RemoteAddr = net.JoinHostPort(ip.String(), "0")
				r = r2
			}
			next.ServeHTTP(w, r)
		})
	}
}

func ipFromHeader(v string) (netip.Addr, bool) {
	if i := strings.LastIndexByte(v, ','); i >= 0 {
		v = v[i+1:]
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(v))
	if err != nil {
		return netip.Addr{}, false
	}
	return ip.Unmap(), true
}
