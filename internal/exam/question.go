package exam

import (
	"time"
)

// Kind adalah jenis soal (T128).
type Kind string

const (
	KindMC       Kind = "pg"          // pilihan ganda, satu benar
	KindMulti    Kind = "pg_kompleks" // beberapa opsi benar
	KindTF       Kind = "benar_salah" // beberapa pernyataan B/S
	KindShort    Kind = "isian"       // isian singkat
	KindMatch    Kind = "menjodohkan" // pasangan kiri-kanan
	KindEssay    Kind = "esai"        // dinilai manual (T147)
	maxChoices        = 6
	minChoices        = 2
	maxStatement      = 10
	maxPairs          = 10
)

// Kinds adalah semua jenis soal dalam urutan tampil.
var Kinds = []Kind{KindMC, KindMulti, KindTF, KindShort, KindMatch, KindEssay}

var kindLabels = map[Kind]string{
	KindMC: "Pilihan ganda", KindMulti: "PG kompleks", KindTF: "Benar/salah",
	KindShort: "Isian singkat", KindMatch: "Menjodohkan", KindEssay: "Esai",
}

// Label mengembalikan nama jenis untuk tampilan.
func (k Kind) Label() string { return kindLabels[k] }

// Valid melaporkan apakah k jenis yang dikenal.
func (k Kind) Valid() bool { _, ok := kindLabels[k]; return ok }

// Tingkat kesulitan dan level kognitif.
var (
	Difficulties    = []string{"mudah", "sedang", "sulit"}
	CognitiveLevels = []string{"C1", "C2", "C3", "C4", "C5", "C6"}
)

// CognitiveGroup mengelompokkan level kognitif: C1–C2 LOTS, C3 MOTS,
// C4–C6 HOTS.
func CognitiveGroup(level string) string {
	switch level {
	case "C1", "C2":
		return "LOTS"
	case "C3":
		return "MOTS"
	case "C4", "C5", "C6":
		return "HOTS"
	}
	return ""
}

// Status soal.
const (
	StatusDraft    = "draf"
	StatusInReview = "ditelaah"
	StatusReady    = "siap"
)

// Choice adalah satu opsi, pernyataan, atau item pasangan. Src adalah
// sumber dari guru, HTML hasil render server.
type Choice struct {
	ID   string `json:"id"`
	Src  string `json:"src"`
	HTML string `json:"html"`
}

// Options adalah bagian soal yang boleh dilihat peserta (kolom options).
type Options struct {
	Choices    []Choice `json:"choices,omitempty"`    // pg, pg_kompleks
	Statements []Choice `json:"statements,omitempty"` // benar_salah
	Left       []Choice `json:"left,omitempty"`       // menjodohkan
	Right      []Choice `json:"right,omitempty"`      // menjodohkan (termasuk pengecoh)
	Input      string   `json:"input,omitempty"`      // isian: "teks" atau "angka"
}

// Penilaian PG kompleks.
const (
	ScoringAll          = "semua"        // semua benar = penuh, selain itu 0
	ScoringProportional = "proporsional" // tanpa nilai negatif
)

// AnswerKey adalah kunci jawaban (kolom answer_key). Tidak pernah dikirim
// ke perangkat peserta; penilaian selalu di server.
type AnswerKey struct {
	Correct         []string          `json:"correct,omitempty"`   // pg, pg_kompleks
	Scoring         string            `json:"scoring,omitempty"`   // pg_kompleks
	Truth           map[string]bool   `json:"truth,omitempty"`     // benar_salah
	Accepted        []string          `json:"accepted,omitempty"`  // isian
	Tolerance       float64           `json:"tolerance,omitempty"` // isian angka
	ArabicNormalize bool              `json:"arabic_normalize,omitempty"`
	Pairs           map[string]string `json:"pairs,omitempty"` // menjodohkan: kiri → kanan
	RubricSrc       string            `json:"rubric_src,omitempty"`
	RubricHTML      string            `json:"rubric_html,omitempty"`
}

// Question adalah satu soal di bank.
type Question struct {
	ID              string
	SchoolID        string
	BankID          string
	StimulusID      string
	StimulusOrder   int
	Kind            Kind
	BodySrc         string
	BodyHTML        string
	Options         Options
	Key             AnswerKey
	Points          int
	Difficulty      string
	CognitiveLevel  string
	ObjectiveID     string
	Indicator       string
	Tags            []string
	ExplanationSrc  string
	ExplanationHTML string
	Status          string
	Version         int
	CreatedBy       string
	UpdatedBy       string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	ArchivedAt      *time.Time
}

// PublicChoice adalah opsi untuk peserta: hanya ID dan HTML.
type PublicChoice struct {
	ID   string `json:"id"`
	HTML string `json:"html"`
}

// ParticipantQuestion adalah soal yang dikirim ke perangkat peserta. Tipe
// ini sengaja tidak punya field kunci, pembahasan, rubrik, atau sumber.
type ParticipantQuestion struct {
	ID         string         `json:"id"`
	Kind       Kind           `json:"kind"`
	Body       string         `json:"body"`
	Points     int            `json:"points"`
	StimulusID string         `json:"stimulus_id,omitempty"`
	Choices    []PublicChoice `json:"choices,omitempty"`
	Statements []PublicChoice `json:"statements,omitempty"`
	Left       []PublicChoice `json:"left,omitempty"`
	Right      []PublicChoice `json:"right,omitempty"`
	Input      string         `json:"input,omitempty"`
	// Multiple: PG kompleks (peserta boleh memilih lebih dari satu).
	Multiple bool `json:"multiple,omitempty"`
}

func public(cs []Choice) []PublicChoice {
	if len(cs) == 0 {
		return nil
	}
	out := make([]PublicChoice, len(cs))
	for i, c := range cs {
		out[i] = PublicChoice{ID: c.ID, HTML: c.HTML}
	}
	return out
}

// ForParticipant membuat versi soal untuk peserta. Satu-satunya jalan
// serialisasi soal ke peserta (halaman ujian, payload offline).
func ForParticipant(q Question) ParticipantQuestion {
	p := ParticipantQuestion{ID: q.ID, Kind: q.Kind, Body: q.BodyHTML, Points: q.Points, StimulusID: q.StimulusID}
	switch q.Kind {
	case KindMC, KindMulti:
		p.Choices = public(q.Options.Choices)
		p.Multiple = q.Kind == KindMulti
	case KindTF:
		p.Statements = public(q.Options.Statements)
	case KindMatch:
		p.Left, p.Right = public(q.Options.Left), public(q.Options.Right)
	case KindShort:
		p.Input = q.Options.Input
		if p.Input == "" {
			p.Input = "teks"
		}
	}
	return p
}

// Excerpt mengembalikan potongan teks soal untuk daftar.
func (q Question) Excerpt() string { return Snippet(PlainText(q.BodySrc), 120) }
