package exam

import (
	"context"
	"testing"

	"akra/internal/platform/database"
	"akra/internal/platform/module"
)

// setExamModule mengatur status modul ujian sekolah langsung di database
// untuk uji (tanpa penjaga module.Admin, mis. mematikan modul saat sesi
// berjalan), lalu membuang cache status modul.
func setExamModule(t testing.TB, q database.DBTX, schoolID string, m Module) {
	t.Helper()
	if _, err := q.Exec(context.Background(), `INSERT INTO school_modules (school_id, module, enabled, options)
		VALUES ($1, 'exam', $2, jsonb_build_object('release', $3::text))
		ON CONFLICT (school_id, module) DO UPDATE SET enabled = EXCLUDED.enabled, options = EXCLUDED.options`,
		schoolID, m.Enabled, string(m.Release)); err != nil {
		t.Fatal(err)
	}
	module.Forget(schoolID)
}
