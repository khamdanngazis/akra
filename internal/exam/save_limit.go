package exam

import (
	"sync"
	"time"
)

// Pembatas simpan jawaban per attempt (T153): token bucket di memori tiap
// replika. Klien normal menyimpan ±1 kali per 20 detik (lebih sering saat
// menjawab cepat); banjir permintaan dari satu attempt dijawab 429 dan
// klien mengulang dengan jeda bertambah (antrian perangkat tetap utuh).
const (
	saveBurst     = 20          // permintaan beruntun yang boleh
	saveRefill    = time.Second // satu token kembali tiap detik
	saveIdleSweep = 10 * time.Minute
)

type saveBucket struct {
	tokens float64
	last   time.Time
}

type saveLimiter struct {
	mu      sync.Mutex
	buckets map[string]*saveBucket
	swept   time.Time
}

func newSaveLimiter() *saveLimiter { return &saveLimiter{buckets: map[string]*saveBucket{}} }

// allow melaporkan apakah attempt boleh menyimpan pada now.
func (l *saveLimiter) allow(attemptID string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.swept) > saveIdleSweep {
		for id, b := range l.buckets {
			if now.Sub(b.last) > saveIdleSweep {
				delete(l.buckets, id)
			}
		}
		l.swept = now
	}
	b, ok := l.buckets[attemptID]
	if !ok {
		b = &saveBucket{tokens: saveBurst, last: now}
		l.buckets[attemptID] = b
	}
	b.tokens = min(saveBurst, b.tokens+float64(now.Sub(b.last))/float64(saveRefill))
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}
