package main

import (
	"bytes"
	"io"
	"net/http"
)

func readAll(r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	return io.ReadAll(r.Body)
}

func ioNopCloser(b []byte) io.ReadCloser { return io.NopCloser(bytes.NewReader(b)) }

// bufferedWriter menampung balasan backend agar backend lain bisa dicoba
// bila sambungan gagal sebelum ada balasan.
type bufferedWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (b *bufferedWriter) Header() http.Header { return b.header }

func (b *bufferedWriter) WriteHeader(code int) {
	if b.status == 0 {
		b.status = code
	}
}

func (b *bufferedWriter) Write(p []byte) (int, error) {
	if b.status == 0 {
		b.status = http.StatusOK
	}
	return b.body.Write(p)
}
