package exam

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"akra/internal/auth"
	"akra/internal/exam/mathrender"
	"akra/internal/platform/database"
	"akra/internal/platform/settings"
	"akra/internal/platform/storage"
)

// Konten contoh untuk sekolah baru (T154): satu bank contoh Matematika per
// jenjang yang ada di sekolah (dari tingkat kelas; tanpa kelas: ketiganya),
// berisi semua jenis soal termasuk rumus dan gambar, ditambah ujian latihan
// bawaan (T136). Dibuat sekali saat staf pertama membuka modul ujian;
// bank contoh yang diarsipkan sekolah tidak dibuat ulang.

// SamplesSeeded adalah kunci pengaturan sekolah penanda konten contoh sudah
// dibuat (nilainya waktu pembuatan).
const SamplesSeeded = "exam.samples_seeded"

// sampleLevel adalah isi bank contoh satu jenjang.
type sampleLevel struct {
	Stage     string // SD, SMP, SMA
	Grade     int    // tingkat bank contoh
	MinGrade  int    // jenjang dipakai bila sekolah punya kelas MinGrade..MaxGrade
	MaxGrade  int
	Chart     sampleChart
	Questions []url.Values
}

// sampleChart adalah diagram batang untuk soal bergambar.
type sampleChart struct {
	Title  string
	Labels []string
	Values []int
	Alt    string
}

// imageQuestion adalah penanda isi soal bergambar: diganti tag img setelah
// gambar disimpan.
const imageQuestion = "{{gambar}}"

var sampleLevels = []sampleLevel{
	{Stage: "SD", Grade: 4, MinGrade: 1, MaxGrade: 6,
		Chart: sampleChart{Title: "Buah kesukaan siswa kelas 4", Labels: []string{"Apel", "Jeruk", "Pisang", "Mangga"}, Values: []int{8, 5, 10, 6},
			Alt: "Diagram batang buah kesukaan: apel 8, jeruk 5, pisang 10, mangga 6 siswa"},
		Questions: []url.Values{
			{"kind": {"pg"}, "body": {`Hasil dari \(125 + 275\) adalah …`}, "choice_0": {"400"}, "choice_1": {"390"}, "choice_2": {"300"}, "choice_3": {"410"},
				"correct": {"0"}, "difficulty": {"mudah"}, "explanation": {`Jumlahkan satuan, puluhan, lalu ratusan: \(125 + 275 = 400\).`}},
			{"kind": {"pg"}, "body": {"<p>Perhatikan diagram berikut.</p>" + imageQuestion + "<p>Buah yang paling banyak disukai adalah …</p>"},
				"choice_0": {"Apel"}, "choice_1": {"Jeruk"}, "choice_2": {"Pisang"}, "choice_3": {"Mangga"}, "correct": {"2"}, "difficulty": {"mudah"},
				"explanation": {"Batang pisang paling tinggi, yaitu 10 siswa."}},
			{"kind": {"pg_kompleks"}, "body": {"Pilih semua bangun datar yang memiliki empat sisi."}, "choice_0": {"Persegi"}, "choice_1": {"Segitiga"},
				"choice_2": {"Persegi panjang"}, "choice_3": {"Lingkaran"}, "correct": {"0", "2"}, "scoring": {"proporsional"}, "difficulty": {"sedang"}},
			{"kind": {"benar_salah"}, "body": {"Tentukan benar atau salah pernyataan berikut."}, "statement_0": {"7 × 8 = 56"}, "truth_0": {"B"},
				"statement_1": {"Setengah dari 30 adalah 20."}, "truth_1": {"S"}, "difficulty": {"mudah"}},
			{"kind": {"isian"}, "body": {"Keliling persegi dengan panjang sisi 6 cm adalah … cm."}, "accepted": {"24"}, "input": {"angka"}, "difficulty": {"sedang"},
				"explanation": {`Keliling persegi \(= 4 \times s = 4 \times 6 = 24\) cm.`}},
			{"kind": {"menjodohkan"}, "body": {"Jodohkan bangun datar dengan banyak sisinya."}, "left_0": {"Segitiga"}, "right_0": {"3"},
				"left_1": {"Persegi"}, "right_1": {"4"}, "left_2": {"Segi lima"}, "right_2": {"5"}, "distractor_0": {"6"}, "difficulty": {"mudah"}},
			{"kind": {"esai"}, "body": {"Ibu membeli 3 kantong jeruk. Setiap kantong berisi 12 jeruk. Jeruk dibagikan sama banyak kepada 4 anak. Berapa jeruk yang diterima setiap anak? Tuliskan cara pengerjaanmu."},
				"rubric": {"Menghitung 3 × 12 = 36 (2 poin), membagi 36 : 4 = 9 (2 poin), jawaban 9 jeruk (1 poin)."}, "points": {"5"}, "difficulty": {"sedang"}},
		}},
	{Stage: "SMP", Grade: 8, MinGrade: 7, MaxGrade: 9,
		Chart: sampleChart{Title: "Rata-rata nilai ulangan kelas 8", Labels: []string{"8A", "8B", "8C", "8D"}, Values: []int{78, 85, 72, 80},
			Alt: "Diagram batang rata-rata nilai: 8A 78, 8B 85, 8C 72, 8D 80"},
		Questions: []url.Values{
			{"kind": {"pg"}, "body": {`Nilai \(x\) yang memenuhi \(2x + 5 = 17\) adalah …`}, "choice_0": {"6"}, "choice_1": {"11"}, "choice_2": {"7"}, "choice_3": {"5"},
				"correct": {"0"}, "difficulty": {"mudah"}, "explanation": {`\(2x = 17 - 5 = 12\), maka \(x = 6\).`}},
			{"kind": {"pg"}, "body": {"<p>Perhatikan diagram berikut.</p>" + imageQuestion + "<p>Selisih rata-rata nilai tertinggi dan terendah adalah …</p>"},
				"choice_0": {"7"}, "choice_1": {"13"}, "choice_2": {"8"}, "choice_3": {"5"}, "correct": {"1"}, "difficulty": {"sedang"},
				"explanation": {`Tertinggi 8B (85), terendah 8C (72): \(85 - 72 = 13\).`}},
			{"kind": {"pg_kompleks"}, "body": {"Pilih semua bilangan prima."}, "choice_0": {"2"}, "choice_1": {"9"}, "choice_2": {"13"}, "choice_3": {"15"},
				"correct": {"0", "2"}, "scoring": {"proporsional"}, "difficulty": {"sedang"}},
			{"kind": {"benar_salah"}, "body": {"Tentukan benar atau salah pernyataan berikut."}, "statement_0": {"√49 = 7"}, "truth_0": {"B"},
				"statement_1": {"Besar sudut siku-siku adalah 180°."}, "truth_1": {"S"}, "difficulty": {"mudah"}},
			{"kind": {"isian"}, "body": {"Panjang sisi miring segitiga siku-siku dengan sisi tegak 6 cm dan 8 cm adalah … cm."}, "accepted": {"10"}, "input": {"angka"},
				"difficulty": {"sedang"}, "explanation": {`Teorema Pythagoras: \(\sqrt{6^2 + 8^2} = \sqrt{100} = 10\).`}},
			{"kind": {"menjodohkan"}, "body": {"Jodohkan bangun ruang dengan rumus volumenya."}, "left_0": {"Kubus"}, "right_0": {"sisi × sisi × sisi"},
				"left_1": {"Balok"}, "right_1": {"panjang × lebar × tinggi"}, "left_2": {"Tabung"}, "right_2": {"luas alas × tinggi"},
				"distractor_0": {"½ × alas × tinggi"}, "difficulty": {"sedang"}},
			{"kind": {"esai"}, "body": {"Harga 3 buku dan 2 pensil Rp21.000. Harga 1 buku dan 2 pensil Rp11.000. Tentukan harga satu buku. Tuliskan langkah pengerjaanmu."},
				"rubric": {"Menyusun dua persamaan (2 poin), eliminasi: 2 buku = Rp10.000 (2 poin), harga satu buku Rp5.000 (1 poin)."}, "points": {"5"}, "difficulty": {"sulit"}},
		}},
	{Stage: "SMA", Grade: 11, MinGrade: 10, MaxGrade: 12,
		Chart: sampleChart{Title: "Produksi padi desa (ton)", Labels: []string{"2022", "2023", "2024", "2025"}, Values: []int{40, 45, 53, 49},
			Alt: "Diagram batang produksi padi: 2022 40 ton, 2023 45 ton, 2024 53 ton, 2025 49 ton"},
		Questions: []url.Values{
			{"kind": {"pg"}, "body": {`Turunan pertama dari \(f(x) = 3x^2 + 2x\) adalah …`}, "choice_0": {`\(6x + 2\)`}, "choice_1": {`\(3x + 2\)`},
				"choice_2": {`\(6x\)`}, "choice_3": {`\(6x^2 + 2\)`}, "correct": {"0"}, "difficulty": {"mudah"},
				"explanation": {`\(\frac{d}{dx}(3x^2) = 6x\) dan \(\frac{d}{dx}(2x) = 2\).`}},
			{"kind": {"pg"}, "body": {"<p>Perhatikan diagram berikut.</p>" + imageQuestion + "<p>Kenaikan produksi terbesar terjadi pada periode …</p>"},
				"choice_0": {"2022–2023"}, "choice_1": {"2023–2024"}, "choice_2": {"2024–2025"}, "choice_3": {"Tidak ada kenaikan"}, "correct": {"1"}, "difficulty": {"sedang"},
				"explanation": {"2022–2023 naik 5 ton, 2023–2024 naik 8 ton, 2024–2025 turun 4 ton."}},
			{"kind": {"pg_kompleks"}, "body": {"Pilih semua fungsi yang selalu naik pada seluruh bilangan real."}, "choice_0": {`\(f(x) = 2x + 1\)`},
				"choice_1": {`\(f(x) = x^2\)`}, "choice_2": {`\(f(x) = x^3\)`}, "choice_3": {`\(f(x) = -x\)`}, "correct": {"0", "2"}, "scoring": {"proporsional"}, "difficulty": {"sulit"}},
			{"kind": {"benar_salah"}, "body": {"Tentukan benar atau salah pernyataan berikut."}, "statement_0": {"log 1000 = 3"}, "truth_0": {"B"},
				"statement_1": {"sin 90° = 0"}, "truth_1": {"S"}, "difficulty": {"mudah"}},
			{"kind": {"isian"}, "body": {`Nilai dari \(\lim_{x \to 2} (3x + 4)\) adalah …`}, "accepted": {"10"}, "input": {"angka"}, "difficulty": {"mudah"},
				"explanation": {`Substitusi langsung: \(3 \cdot 2 + 4 = 10\).`}},
			{"kind": {"menjodohkan"}, "body": {"Jodohkan fungsi dengan turunan pertamanya."}, "left_0": {"x²"}, "right_0": {"2x"},
				"left_1": {"sin x"}, "right_1": {"cos x"}, "left_2": {"ln x"}, "right_2": {"1/x"}, "distractor_0": {"−sin x"}, "difficulty": {"sedang"}},
			{"kind": {"esai"}, "body": {"Sebuah persegi panjang memiliki keliling 40 cm. Tentukan panjang dan lebar yang membuat luasnya paling besar, lalu jelaskan alasanmu."},
				"rubric": {"Menyatakan luas L = x(20 − x) (2 poin), mencari maksimum di x = 10 (2 poin), menyimpulkan persegi 10 cm × 10 cm (1 poin)."}, "points": {"5"}, "difficulty": {"sulit"}},
		}},
}

// sampleStages mengembalikan jenjang contoh untuk sekolah: dari tingkat
// kelas yang ada, atau semua jenjang bila belum ada kelas.
func sampleStages(ctx context.Context, q database.DBTX, schoolID string) ([]sampleLevel, error) {
	rows, err := q.Query(ctx, `SELECT DISTINCT grade_level FROM classes WHERE school_id = $1`, schoolID)
	if err != nil {
		return nil, fmt.Errorf("exam: sample stages: %w", err)
	}
	grades, err := pgx.CollectRows(rows, pgx.RowTo[int16])
	if err != nil {
		return nil, fmt.Errorf("exam: sample stages: %w", err)
	}
	if len(grades) == 0 {
		return sampleLevels, nil
	}
	var out []sampleLevel
	for _, l := range sampleLevels {
		for _, g := range grades {
			if int(g) >= l.MinGrade && int(g) <= l.MaxGrade {
				out = append(out, l)
				break
			}
		}
	}
	return out, nil
}

// sampleOwner mengembalikan admin_tu aktif pertama sekolah sebagai pembuat
// konten contoh (agar guru yang kebetulan membuka pertama tidak menjadi
// pemiliknya). ok false bila sekolah belum punya admin_tu.
func sampleOwner(ctx context.Context, q database.DBTX, schoolID string) (StaffActor, bool, error) {
	var id string
	err := q.QueryRow(ctx, `SELECT u.id FROM users u JOIN user_roles ur ON ur.user_id = u.id JOIN roles r ON r.id = ur.role_id
		WHERE u.school_id = $1 AND u.is_active AND r.code = $2 ORDER BY u.created_at, u.id LIMIT 1`, schoolID, auth.RoleAdminTU).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return StaffActor{}, false, nil
	}
	if err != nil {
		return StaffActor{}, false, fmt.Errorf("exam: sample owner: %w", err)
	}
	return StaffActor{Actor: Actor{UserID: id, Roles: []string{auth.RoleAdminTU}}, SchoolID: schoolID}, true, nil
}

// EnsureSamples membuat konten contoh sekolah bila belum pernah dibuat:
// bank contoh per jenjang dan ujian latihan bawaan. Mengembalikan true bila
// konten dibuat pada panggilan ini. store boleh nil (soal bergambar
// dilewati).
func EnsureSamples(ctx context.Context, db database.DB, store storage.Store, math *mathrender.Renderer, schoolID string, now time.Time) (bool, error) {
	if _, done, err := settings.Get(ctx, db, schoolID, SamplesSeeded); err != nil || done {
		return false, err
	}
	owner, ok, err := sampleOwner(ctx, db, schoolID)
	if err != nil || !ok {
		return false, err
	}
	created := false
	err = pgx.BeginFunc(ctx, db, func(tx pgx.Tx) error {
		// Kunci per sekolah: dua staf membuka bersamaan tidak membuat ganda.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext('exam_samples:' || $1))`, schoolID); err != nil {
			return fmt.Errorf("exam: lock samples: %w", err)
		}
		if _, done, err := settings.Get(ctx, tx, schoolID, SamplesSeeded); err != nil || done {
			return err
		}
		levels, err := sampleStages(ctx, tx, schoolID)
		if err != nil {
			return err
		}
		var subjectID *string
		var sid string
		err = tx.QueryRow(ctx, `SELECT id FROM subjects WHERE school_id = $1 AND name ILIKE 'matematika%' ORDER BY name LIMIT 1`, schoolID).Scan(&sid)
		switch {
		case err == nil:
			subjectID = &sid
		case !errors.Is(err, pgx.ErrNoRows):
			return fmt.Errorf("exam: sample subject: %w", err)
		}
		for _, l := range levels {
			if err := seedSampleBank(ctx, tx, store, math, owner, subjectID, l, now); err != nil {
				return err
			}
		}
		if err := settings.Set(ctx, tx, schoolID, SamplesSeeded, now.UTC().Format(time.RFC3339)); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if _, err := EnsurePractice(ctx, db, owner, math, now); err != nil {
		return created, err
	}
	return created, nil
}

// seedSampleBank membuat satu bank contoh dengan soal-soalnya.
func seedSampleBank(ctx context.Context, tx pgx.Tx, store storage.Store, math *mathrender.Renderer, owner StaffActor, subjectID *string, l sampleLevel, now time.Time) error {
	var bankID string
	name := fmt.Sprintf("Contoh — Matematika %s Kelas %d", l.Stage, l.Grade)
	if err := tx.QueryRow(ctx, `INSERT INTO exam_banks (school_id, name, subject_id, grade_level, created_by, sample) VALUES ($1, $2, $3, $4, $5, true) RETURNING id`,
		owner.SchoolID, name, subjectID, l.Grade, owner.UserID).Scan(&bankID); err != nil {
		return fmt.Errorf("exam: sample bank: %w", err)
	}
	var img string
	if store != nil {
		data, err := l.Chart.PNG()
		if err != nil {
			return err
		}
		a, err := SaveAsset(ctx, tx, store, owner, bankID, data)
		if err != nil {
			return err
		}
		img = `<img data-asset="` + a.ID + `" alt="` + html.EscapeString(l.Chart.Alt) + `">`
	}
	for _, v := range l.Questions {
		f := ParseQuestionForm(v)
		if strings.Contains(f.Body, imageQuestion) {
			if img == "" {
				continue
			}
			f.Body = strings.ReplaceAll(f.Body, imageQuestion, img)
		}
		f.Tags = "contoh"
		f.Status = StatusReady
		q, _, err := BuildQuestion(f, math)
		if err != nil {
			return fmt.Errorf("exam: sample question %s: %w", l.Stage, err)
		}
		if _, err := createQuestionTx(ctx, tx, owner, bankID, q, now); err != nil {
			return err
		}
	}
	return nil
}

// PNG menggambar diagram batang sederhana (tanpa berkas aset).
func (c sampleChart) PNG() ([]byte, error) {
	const w, h, left, bottom, top = 640, 400, 60, 50, 50
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.White, image.Point{}, draw.Src)
	ink := color.RGBA{15, 23, 42, 255}
	grid := color.RGBA{226, 232, 240, 255}
	bar := color.RGBA{13, 148, 136, 255}
	fill := func(x0, y0, x1, y1 int, col color.Color) {
		draw.Draw(img, image.Rect(x0, y0, x1, y1), &image.Uniform{col}, image.Point{}, draw.Src)
	}
	text := func(x, y int, s string) {
		d := font.Drawer{Dst: img, Src: &image.Uniform{ink}, Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
		d.DrawString(s)
	}
	width := func(s string) int { return font.MeasureString(basicfont.Face7x13, s).Round() }
	maxV := 0
	for _, v := range c.Values {
		maxV = max(maxV, v)
	}
	// Skala sumbu: kelipatan 2, 5, atau 10 terdekat di atas nilai terbesar.
	step := 1
	for _, s := range []int{1, 2, 5, 10, 20, 50, 100} {
		step = s
		if maxV/s <= 6 {
			break
		}
	}
	top10 := (maxV/step + 1) * step
	plotH := h - top - bottom
	y := func(v int) int { return h - bottom - v*plotH/top10 }
	for v := 0; v <= top10; v += step {
		fill(left, y(v), w-20, y(v)+1, grid)
		s := strconv.Itoa(v)
		text(left-8-width(s), y(v)+4, s)
	}
	fill(left, top, left+2, h-bottom, ink)
	fill(left, h-bottom, w-20, h-bottom+2, ink)
	n := len(c.Values)
	slot := (w - 20 - left) / max(n, 1)
	for i, v := range c.Values {
		x0 := left + i*slot + slot/4
		x1 := left + (i+1)*slot - slot/4
		fill(x0, y(v), x1, h-bottom, bar)
		s := strconv.Itoa(v)
		text((x0+x1-width(s))/2, y(v)-6, s)
		text((x0+x1-width(c.Labels[i]))/2, h-bottom+20, c.Labels[i])
	}
	text((w-width(c.Title))/2, 28, c.Title)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("exam: sample chart: %w", err)
	}
	return buf.Bytes(), nil
}
