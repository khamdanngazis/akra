package exam

import (
	"context"
	"encoding/json"
	"fmt"

	"akra/internal/platform/database"
)

// PaperItem adalah satu soal di naskah satu varian, berurutan.
type PaperItem struct {
	Number    int
	SectionID string
	Question  Question
	Stimulus  *Stimulus
	Points    int
	// FirstOfStimulus: soal pertama kelompok stimulusnya (stimulus dicetak
	// sekali di atas kelompok).
	FirstOfStimulus bool
}

// LoadPaper mengembalikan naskah varian variantID ujian e. Ujian yang sudah
// terkunci dibaca dari snapshot (versi soal saat terkunci); sebelum itu
// dari isi bank saat ini.
func LoadPaper(ctx context.Context, q database.DBTX, e Exam, variantID string) ([]PaperItem, error) {
	query := `SELECT vi.section_id, q.points, to_jsonb(q.*), st.id IS NOT NULL, coalesce(to_jsonb(st.*), '{}')
		FROM exam_variant_items vi
		JOIN exam_variants v ON v.id = vi.variant_id
		JOIN exam_questions q ON q.id = vi.question_id AND q.school_id = vi.school_id
		LEFT JOIN exam_stimuli st ON st.id = vi.stimulus_id AND st.school_id = vi.school_id
		WHERE vi.school_id = $1 AND v.exam_id = $2 AND vi.variant_id = $3
		ORDER BY vi.position`
	if e.Locked() {
		query = `SELECT s.section_id, s.points, qv.snapshot, sv.snapshot IS NOT NULL, coalesce(sv.snapshot, '{}')
			FROM exam_item_snapshots s
			JOIN exam_question_versions qv ON qv.question_id = s.question_id AND qv.version = s.question_version
			LEFT JOIN exam_stimulus_versions sv ON sv.stimulus_id = s.stimulus_id AND sv.version = s.stimulus_version
			WHERE s.school_id = $1 AND s.exam_id = $2 AND s.variant_id = $3
			ORDER BY s.position`
	}
	rows, err := q.Query(ctx, query, e.SchoolID, e.ID, variantID)
	if err != nil {
		return nil, fmt.Errorf("exam: load paper: %w", err)
	}
	defer rows.Close()
	var out []PaperItem
	stims := map[string]*Stimulus{}
	for rows.Next() {
		var it PaperItem
		var qraw, sraw []byte
		var hasStim bool
		var pts int16
		if err := rows.Scan(&it.SectionID, &pts, &qraw, &hasStim, &sraw); err != nil {
			return nil, fmt.Errorf("exam: load paper: %w", err)
		}
		it.Points = int(pts)
		if e.Locked() {
			if err := json.Unmarshal(qraw, &it.Question); err != nil {
				return nil, fmt.Errorf("exam: decode paper question: %w", err)
			}
		} else if it.Question, err = questionFromRow(qraw); err != nil {
			return nil, err
		}
		if hasStim {
			var s Stimulus
			if e.Locked() {
				err = json.Unmarshal(sraw, &s)
			} else {
				s, err = stimulusFromRow(sraw)
			}
			if err != nil {
				return nil, fmt.Errorf("exam: decode paper stimulus: %w", err)
			}
			if prev, ok := stims[s.ID]; ok {
				it.Stimulus = prev
			} else {
				stims[s.ID] = &s
				it.Stimulus = &s
				it.FirstOfStimulus = true
			}
		}
		it.Number = len(out) + 1
		out = append(out, it)
	}
	return out, rows.Err()
}

// questionFromRow membaca soal dari to_jsonb(exam_questions.*).
func questionFromRow(raw []byte) (Question, error) {
	var r struct {
		ID              string    `json:"id"`
		SchoolID        string    `json:"school_id"`
		BankID          string    `json:"bank_id"`
		StimulusID      *string   `json:"stimulus_id"`
		StimulusOrder   *int      `json:"stimulus_order"`
		Kind            Kind      `json:"kind"`
		BodySrc         string    `json:"body_src"`
		BodyHTML        string    `json:"body_html"`
		Options         Options   `json:"options"`
		Key             AnswerKey `json:"answer_key"`
		Points          int       `json:"points"`
		Difficulty      *string   `json:"difficulty"`
		CognitiveLevel  *string   `json:"cognitive_level"`
		ObjectiveID     *string   `json:"objective_id"`
		Indicator       string    `json:"indicator"`
		ExplanationSrc  string    `json:"explanation_src"`
		ExplanationHTML string    `json:"explanation_html"`
		Status          string    `json:"status"`
		Version         int       `json:"version"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Question{}, fmt.Errorf("exam: decode paper question: %w", err)
	}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return *p
	}
	q := Question{ID: r.ID, SchoolID: r.SchoolID, BankID: r.BankID, StimulusID: str(r.StimulusID), Kind: r.Kind, BodySrc: r.BodySrc,
		BodyHTML: r.BodyHTML, Options: r.Options, Key: r.Key, Points: r.Points, Difficulty: str(r.Difficulty), CognitiveLevel: str(r.CognitiveLevel),
		ObjectiveID: str(r.ObjectiveID), Indicator: r.Indicator, ExplanationSrc: r.ExplanationSrc, ExplanationHTML: r.ExplanationHTML,
		Status: r.Status, Version: r.Version}
	if r.StimulusOrder != nil {
		q.StimulusOrder = *r.StimulusOrder
	}
	return q, nil
}

func stimulusFromRow(raw []byte) (Stimulus, error) {
	var r struct {
		ID       string `json:"id"`
		SchoolID string `json:"school_id"`
		BankID   string `json:"bank_id"`
		Title    string `json:"title"`
		BodySrc  string `json:"body_src"`
		BodyHTML string `json:"body_html"`
		Numbered bool   `json:"numbered"`
		Version  int    `json:"version"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return Stimulus{}, err
	}
	return Stimulus{ID: r.ID, SchoolID: r.SchoolID, BankID: r.BankID, Title: r.Title, BodySrc: r.BodySrc, BodyHTML: r.BodyHTML,
		Numbered: r.Numbered, Version: r.Version}, nil
}
