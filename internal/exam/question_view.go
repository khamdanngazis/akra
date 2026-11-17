package exam

import (
	"html/template"

	"akra/internal/exam/richtext"
)

// ChoiceView adalah satu opsi/pernyataan/item untuk ditampilkan.
type ChoiceView struct {
	ID     string
	Letter string
	HTML   template.HTML
}

// QuestionView adalah soal dalam tampilan peserta (pratinjau guru dan
// halaman ujian T138). Hanya dibuat dari ParticipantQuestion, sehingga
// tidak pernah memuat kunci.
type QuestionView struct {
	ID         string
	Number     int
	Kind       Kind
	Body       template.HTML
	Points     int
	Multiple   bool
	Input      string
	Choices    []ChoiceView
	Statements []ChoiceView
	Left       []ChoiceView
	Right      []ChoiceView
	// Stimulus (opsional): tampil di kolom kiri pada layar lebar.
	StimulusTitle string
	Stimulus      template.HTML
	Numbered      bool
}

// HTML hasil Process aman dipakai apa adanya; gambar diberi URL lewat
// resolve.
func trusted(h string, resolve func(string) string) template.HTML {
	return template.HTML(richtext.ResolveAssets(h, resolve)) //nolint:gosec // HTML buatan richtext.Process
}

func choiceViews(cs []PublicChoice, resolve func(string) string, letters bool) []ChoiceView {
	out := make([]ChoiceView, len(cs))
	for i, c := range cs {
		out[i] = ChoiceView{ID: c.ID, HTML: trusted(c.HTML, resolve)}
		if letters {
			out[i].Letter = letter(i)
		}
	}
	return out
}

// NewQuestionView menyusun tampilan peserta untuk soal nomor number.
func NewQuestionView(pq ParticipantQuestion, number int, resolve func(string) string) QuestionView {
	return QuestionView{ID: pq.ID, Number: number, Kind: pq.Kind, Body: trusted(pq.Body, resolve), Points: pq.Points,
		Multiple: pq.Multiple, Input: pq.Input,
		Choices:    choiceViews(pq.Choices, resolve, true),
		Statements: choiceViews(pq.Statements, resolve, false),
		Left:       choiceViews(pq.Left, resolve, false),
		Right:      choiceViews(pq.Right, resolve, true),
	}
}

// WithStimulus menambahkan stimulus ke tampilan soal.
func (v QuestionView) WithStimulus(s Stimulus, resolve func(string) string) QuestionView {
	v.StimulusTitle, v.Stimulus, v.Numbered = s.Title, trusted(s.BodyHTML, resolve), s.Numbered
	return v
}

// staffAssetURL adalah URL gambar di halaman staf: route yang memeriksa
// akses lalu mengalihkan ke URL bertanda tangan.
func staffAssetURL(id string) string { return "/ujian/aset/" + id }
