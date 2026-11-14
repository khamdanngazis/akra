package dapodik

import (
	"bytes"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// Format import nilai Dapodik 2027 (template-format-import-nilai.xlsx,
// sheet "Format Import Nilai"): kepala di baris 8–14 (label kolom A, nilai
// kolom E), header baris 16–17, data mulai baris 18:
// A No, B Token Nilai (tersembunyi), C Token Anggota Rombel, D Nama, E NIS,
// F NISN, G/H Pengetahuan nilai/predikat, I/J Keterampilan nilai/predikat,
// K/L Sikap spiritual, M/N Sosial.
const (
	gradeSheet    = "Format Import Nilai"
	gradeFirstRow = 18
	maxGradeRows  = 200
)

// ErrNotGradeFormat dikembalikan jika file bukan format import nilai
// Dapodik.
var ErrNotGradeFormat = errors.New("dapodik: not a Dapodik grade import file")

// GradeFileInfo adalah kepala file format import nilai.
type GradeFileInfo struct {
	Subject   string // Mata Evaluasi
	Grade     string // Tingkat Pendidikan
	ClassName string // Rombongan Belajar
	Semester  string // Rapor Ke-
	KKM       string // KKM Pengetahuan
}

// GradeFileRow adalah satu siswa di file.
type GradeFileRow struct {
	Row         int
	Name        string
	NIS, NISN   string
	TokenNilai  string
	TokenRombel string
}

// GradeEntry adalah nilai AKRA untuk satu siswa.
type GradeEntry struct {
	Score  int    // nilai akhir 0–100
	Letter string // predikat; hanya A–D yang ditulis
}

// GradeFillReport adalah ringkasan pengisian.
type GradeFillReport struct {
	Filled     int
	Unmatched  []string // siswa di file yang tidak ditemukan di AKRA
	NoScore    []string // siswa cocok tetapi nilai akhirnya belum lengkap
	LetterSkip bool     // predikat AKRA bukan A–D, kolom predikat tidak diisi
	KKMFilled  bool
}

// ReadGradeFile membaca kepala dan daftar siswa file format import nilai.
func ReadGradeFile(data []byte) (GradeFileInfo, []GradeFileRow, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 50 << 20, UnzipXMLSizeLimit: 50 << 20})
	if err != nil {
		return GradeFileInfo{}, nil, ErrNotGradeFormat
	}
	defer f.Close()
	if idx, err := f.GetSheetIndex(gradeSheet); err != nil || idx < 0 {
		return GradeFileInfo{}, nil, ErrNotGradeFormat
	}
	get := func(cell string) string { v, _ := f.GetCellValue(gradeSheet, cell); return strings.TrimSpace(v) }
	if !strings.EqualFold(get("A8"), "Token Mata Evaluasi") || !strings.Contains(strings.ToLower(get("C16")), "token") {
		return GradeFileInfo{}, nil, ErrNotGradeFormat
	}
	info := GradeFileInfo{Subject: get("E9"), Grade: get("E10"), ClassName: get("E11"), KKM: get("E12"), Semester: get("E14")}
	var rows []GradeFileRow
	for r := gradeFirstRow; r < gradeFirstRow+maxGradeRows; r++ {
		row := strconv.Itoa(r)
		x := GradeFileRow{Row: r, Name: get("D" + row), NIS: get("E" + row), NISN: get("F" + row), TokenNilai: get("B" + row), TokenRombel: get("C" + row)}
		if x.Name == "" && x.NIS == "" && x.TokenRombel == "" {
			break
		}
		rows = append(rows, x)
	}
	return info, rows, nil
}

// FillGradeFile mengisi nilai pengetahuan (G), predikat (H), dan KKM
// pengetahuan (E12, jika kosong) langsung di file asli. Sel lain (token,
// nama, kolom tersembunyi, proteksi, dan validasi data) tidak diubah.
// lookup mencari nilai siswa berdasarkan NISN lalu NIS. Keterampilan dan
// sikap tidak diisi karena AKRA tidak menyimpan nilai terpisah untuk itu.
func FillGradeFile(data []byte, kkm int, lookup func(nisn, nis string) (GradeEntry, bool, bool)) ([]byte, GradeFillReport, error) {
	_, rows, err := ReadGradeFile(data)
	if err != nil {
		return nil, GradeFillReport{}, err
	}
	f, err := excelize.OpenReader(bytes.NewReader(data), excelize.Options{UnzipSizeLimit: 50 << 20, UnzipXMLSizeLimit: 50 << 20})
	if err != nil {
		return nil, GradeFillReport{}, ErrNotGradeFormat
	}
	defer f.Close()
	var rep GradeFillReport
	if v, _ := f.GetCellValue(gradeSheet, "E12"); strings.TrimSpace(v) == "" && kkm > 0 {
		if err := f.SetCellInt(gradeSheet, "E12", kkm); err != nil {
			return nil, rep, err
		}
		rep.KKMFilled = true
	}
	for _, x := range rows {
		label := x.Name + " (NIS " + x.NIS + ")"
		entry, found, complete := lookup(x.NISN, x.NIS)
		switch {
		case !found:
			rep.Unmatched = append(rep.Unmatched, label)
			continue
		case !complete:
			rep.NoScore = append(rep.NoScore, label)
			continue
		}
		row := strconv.Itoa(x.Row)
		if err := f.SetCellInt(gradeSheet, "G"+row, entry.Score); err != nil {
			return nil, rep, err
		}
		switch entry.Letter {
		case "A", "B", "C", "D":
			if err := f.SetCellStr(gradeSheet, "H"+row, entry.Letter); err != nil {
				return nil, rep, err
			}
		default:
			rep.LetterSkip = true
		}
		rep.Filled++
	}
	var out bytes.Buffer
	if err := f.Write(&out); err != nil {
		return nil, rep, err
	}
	return out.Bytes(), rep, nil
}

// readAllLimited membaca r paling banyak n byte.
func readAllLimited(r io.Reader, n int64) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, n+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > n {
		return nil, errors.New("dapodik: file too large")
	}
	return data, nil
}
