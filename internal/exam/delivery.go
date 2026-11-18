package exam

import (
	"context"
	"fmt"
	"html/template"
	"strings"
	"unicode"

	"akra/internal/platform/database"
)

// Payload halaman ujian peserta (T138). Semua soal dikirim sekali saat
// halaman dibuka, disusun menurut urutan attempt. Soal hanya lewat
// ForParticipant, sehingga kunci, pembahasan, rubrik, dan sumber tidak
// pernah ikut.

// DeliverySection adalah satu bagian ujian.
type DeliverySection struct {
	ID           string
	Title        string
	Instructions template.HTML
}

// DeliveryStimulus adalah satu stimulus bersama.
type DeliveryStimulus struct {
	ID       string
	Index    int // nomor kelompok (1, 2, …) untuk penanda grid
	Title    string
	Body     template.HTML
	Numbered bool
	// First dan Last adalah nomor soal pertama dan terakhir kelompok.
	First, Last int
}

// DeliveryQuestion adalah satu soal di halaman ujian.
type DeliveryQuestion struct {
	View       QuestionView
	Index      int // 0-based
	Section    int // indeks di Delivery.Sections
	Stimulus   int // indeks di Delivery.Stimuli + 1; 0 = tanpa stimulus
	NewSection bool
	// RTL: opsi berbahasa Arab, disusun kanan-ke-kiri.
	RTL bool
}

// Delivery adalah isi halaman ujian satu attempt.
type Delivery struct {
	Sections  []DeliverySection
	Stimuli   []DeliveryStimulus
	Questions []DeliveryQuestion
	// HasMath: ada rumus (CSS KaTeX dimuat). Font Arab dimuat browser
	// sendiri lewat unicode-range bila ada huruf Arab.
	HasMath   bool
	HasArabic bool
}

// LoadDelivery menyusun isi halaman ujian attempt a dari snapshot soal.
// resolve memberi URL gambar.
func LoadDelivery(ctx context.Context, q database.DBTX, e Exam, a Attempt, resolve func(string) string) (Delivery, error) {
	paper, err := LoadPaper(ctx, q, e, a.VariantID)
	if err != nil {
		return Delivery{}, err
	}
	byID := make(map[string]PaperItem, len(paper))
	for _, it := range paper {
		byID[it.Question.ID] = it
	}
	var d Delivery
	secIndex := map[string]int{}
	rows, err := q.Query(ctx, `SELECT id, title, instructions_html FROM exam_sections WHERE school_id = $1 AND exam_id = $2 ORDER BY position`,
		e.SchoolID, e.ID)
	if err != nil {
		return Delivery{}, fmt.Errorf("exam: delivery sections: %w", err)
	}
	for rows.Next() {
		var s DeliverySection
		var instr string
		if err := rows.Scan(&s.ID, &s.Title, &instr); err != nil {
			rows.Close()
			return Delivery{}, fmt.Errorf("exam: delivery sections: %w", err)
		}
		s.Instructions = trusted(instr, resolve)
		secIndex[s.ID] = len(d.Sections)
		d.Sections = append(d.Sections, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return Delivery{}, fmt.Errorf("exam: delivery sections: %w", err)
	}
	stimIndex := map[string]int{}
	lastSection := -1
	var raw strings.Builder
	for i, id := range a.QuestionOrder {
		it, ok := byID[id]
		if !ok {
			return Delivery{}, fmt.Errorf("exam: attempt question %s not in paper", id)
		}
		pq := ForParticipant(it.Question)
		if o, ok := a.OptionOrder[id]; ok {
			pq.Choices = reorder(pq.Choices, o.Choices)
			pq.Statements = reorder(pq.Statements, o.Statements)
			pq.Right = reorder(pq.Right, o.Right)
		}
		pq.Points = it.Points
		dq := DeliveryQuestion{View: NewQuestionView(pq, i+1, resolve), Index: i, Section: secIndex[it.SectionID],
			RTL: arabicChoices(pq)}
		if dq.Section != lastSection {
			dq.NewSection, lastSection = true, dq.Section
		}
		raw.WriteString(pq.Body)
		for _, cs := range [][]PublicChoice{pq.Choices, pq.Statements, pq.Left, pq.Right} {
			for _, c := range cs {
				raw.WriteString(c.HTML)
			}
		}
		if s := it.Stimulus; s != nil {
			n, ok := stimIndex[s.ID]
			if !ok {
				d.Stimuli = append(d.Stimuli, DeliveryStimulus{ID: s.ID, Index: len(d.Stimuli) + 1, Title: s.Title,
					Body: trusted(s.BodyHTML, resolve), Numbered: s.Numbered, First: i + 1})
				n = len(d.Stimuli)
				stimIndex[s.ID] = n
				raw.WriteString(s.BodyHTML)
			}
			d.Stimuli[n-1].Last = i + 1
			dq.Stimulus = n
		}
		d.Questions = append(d.Questions, dq)
	}
	for _, s := range d.Sections {
		raw.WriteString(string(s.Instructions))
	}
	all := raw.String()
	d.HasMath = strings.Contains(all, "katex")
	d.HasArabic = strings.IndexFunc(all, func(r rune) bool { return unicode.Is(unicode.Arabic, r) }) >= 0
	return d, nil
}

// arabicChoices melaporkan apakah semua opsi/pernyataan soal diawali huruf
// Arab (opsi tersusun kanan-ke-kiri).
func arabicChoices(pq ParticipantQuestion) bool {
	n := 0
	for _, cs := range [][]PublicChoice{pq.Choices, pq.Statements, pq.Left, pq.Right} {
		for _, c := range cs {
			if !firstStrongArabic(c.HTML) {
				return false
			}
			n++
		}
	}
	return n > 0
}

// firstStrongArabic melaporkan apakah huruf pertama teks HTML h (di luar
// tag) adalah huruf Arab.
func firstStrongArabic(h string) bool {
	inTag := false
	for _, r := range h {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case inTag:
		case unicode.Is(unicode.Arabic, r) && unicode.IsLetter(r):
			return true
		case unicode.IsLetter(r):
			return false
		}
	}
	return false
}
