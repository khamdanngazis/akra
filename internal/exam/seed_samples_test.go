package exam

import (
	"bytes"
	"context"
	"image/png"
	"strings"
	"testing"
	"time"

	"akra/internal/platform/settings"
)

func TestEnsureSamples(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	a := e.a
	var mtk string
	if err := e.tx.QueryRow(ctx, `INSERT INTO subjects (school_id, name, code) VALUES ($1, 'Matematika', 'MTK') RETURNING id`, a.ID).Scan(&mtk); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	created, err := EnsureSamples(ctx, e.tx, e.store, mr(t), a.ID, now)
	if err != nil || !created {
		t.Fatalf("EnsureSamples = %v, %v", created, err)
	}
	// Sekolah A hanya punya kelas 1 dan 2: hanya bank contoh SD.
	type bankRow struct {
		id, name, subject, owner string
		grade                    int
	}
	rows, err := e.tx.Query(ctx, `SELECT id, name, coalesce(subject_id::text, ''), created_by, grade_level FROM exam_banks WHERE school_id = $1 AND sample`, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	var banks []bankRow
	for rows.Next() {
		var b bankRow
		if err := rows.Scan(&b.id, &b.name, &b.subject, &b.owner, &b.grade); err != nil {
			t.Fatal(err)
		}
		banks = append(banks, b)
	}
	rows.Close()
	if len(banks) != 1 || banks[0].grade != 4 || !strings.Contains(banks[0].name, "SD") {
		t.Fatalf("sample banks = %+v", banks)
	}
	b := banks[0]
	if b.subject != mtk || b.owner != a.Admin.ID {
		t.Errorf("sample bank subject %q owner %q, want %q %q", b.subject, b.owner, mtk, a.Admin.ID)
	}
	// Semua jenis soal, siap pakai, bertanda contoh; satu soal bergambar dan
	// soal berumus sudah dirender.
	var kinds, total, ready, tagged, withImage, withMath int
	if err := e.tx.QueryRow(ctx, `SELECT count(DISTINCT kind), count(*), count(*) FILTER (WHERE status = 'siap'),
		count(*) FILTER (WHERE 'contoh' = ANY(tags)), count(*) FILTER (WHERE body_html LIKE '%data-asset=%'),
		count(*) FILTER (WHERE body_html LIKE '%katex%')
		FROM exam_questions WHERE bank_id = $1`, b.id).Scan(&kinds, &total, &ready, &tagged, &withImage, &withMath); err != nil {
		t.Fatal(err)
	}
	if kinds != len(Kinds) || total != 7 || ready != total || tagged != total || withImage != 1 || withMath < 1 {
		t.Errorf("questions: kinds %d total %d ready %d tagged %d image %d math %d", kinds, total, ready, tagged, withImage, withMath)
	}
	if _, ok, err := GetPractice(ctx, e.tx, a.ID); err != nil || !ok {
		t.Errorf("practice exam missing: %v", err)
	}
	if _, ok, _ := settings.Get(ctx, e.tx, a.ID, SamplesSeeded); !ok {
		t.Error("samples marker not set")
	}

	// Bank contoh diarsipkan sekolah: tidak dibuat ulang.
	if _, err := e.tx.Exec(ctx, `UPDATE exam_banks SET archived_at = now() WHERE id = $1`, b.id); err != nil {
		t.Fatal(err)
	}
	if created, err := EnsureSamples(ctx, e.tx, e.store, mr(t), a.ID, now); err != nil || created {
		t.Errorf("second EnsureSamples = %v, %v", created, err)
	}

	// Sekolah tanpa kelas: ketiga jenjang. Tanpa admin_tu: tidak dibuat.
	if _, err := e.tx.Exec(ctx, `DELETE FROM user_roles WHERE user_id IN (SELECT id FROM users WHERE school_id = $1)
		AND role_id = (SELECT id FROM roles WHERE code = 'admin_tu')`, e.b.ID); err != nil {
		t.Fatal(err)
	}
	if created, err := EnsureSamples(ctx, e.tx, e.store, mr(t), e.b.ID, now); err != nil || created {
		t.Errorf("school without admin = %v, %v", created, err)
	}
	stages, err := sampleStages(ctx, e.tx, "00000000-0000-0000-0000-000000000000")
	if err != nil || len(stages) != 3 {
		t.Errorf("stages without classes = %d, %v", len(stages), err)
	}
}

func TestSamplesShownOnFirstVisit(t *testing.T) {
	e := newEnv(t)
	rec := e.get("/ujian/bank/", e.a.Admin)
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), "Contoh — Matematika SD Kelas 4") || !strings.Contains(rec.Body.String(), `badge-info">Contoh`) {
		t.Fatalf("bank list = %d, sample bank not shown", rec.Code)
	}
}

func TestSampleChartPNG(t *testing.T) {
	for _, l := range sampleLevels {
		data, err := l.Chart.PNG()
		if err != nil {
			t.Fatal(err)
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil || img.Bounds().Dx() != 640 {
			t.Fatalf("%s chart: %v", l.Stage, err)
		}
		if _, _, _, _, err := PrepareImage(data); err != nil {
			t.Errorf("%s chart rejected by upload checks: %v", l.Stage, err)
		}
	}
}
