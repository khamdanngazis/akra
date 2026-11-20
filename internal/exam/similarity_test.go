package exam

import "testing"

func TestFindSimilar(t *testing.T) {
	answers := []string{
		// 0: asli.
		"Fotosintesis adalah proses tumbuhan hijau membuat makanan sendiri dengan bantuan cahaya matahari, air, dan karbon dioksida sehingga menghasilkan glukosa dan oksigen.",
		// 1: salinan 0 dengan sedikit ubahan (huruf besar, tanda baca, satu kata).
		"fotosintesis adalah proses tumbuhan hijau membuat makanan sendiri dengan bantuan cahaya matahari air dan karbon dioksida, sehingga menghasilkan glukosa serta oksigen",
		// 2: topik sama, ditulis sendiri.
		"Tumbuhan memakai energi cahaya untuk mengubah air dan CO2 menjadi gula; oksigen dilepas ke udara sebagai hasil sampingan dari proses tersebut.",
		// 3: jawaban pendek: tidak dibandingkan.
		"Proses membuat makanan.",
		"Proses membuat makanan.",
		// 5: salinan 2 dengan urutan kalimat ditukar.
		"Oksigen dilepas ke udara sebagai hasil sampingan dari proses tersebut. Tumbuhan memakai energi cahaya untuk mengubah air dan CO2 menjadi gula.",
		// 6: kosong.
		"",
	}
	got := FindSimilar(answers)
	pairs := map[[2]int]int{}
	for _, p := range got {
		pairs[[2]int{p.A, p.B}] = p.Percent()
	}
	if s, ok := pairs[[2]int{0, 1}]; !ok || s < 70 {
		t.Errorf("copy pair 0-1 not flagged: %v", got)
	}
	if _, ok := pairs[[2]int{2, 5}]; !ok {
		t.Errorf("reordered copy 2-5 not flagged: %v", got)
	}
	for _, p := range [][2]int{{0, 2}, {1, 2}, {3, 4}, {0, 5}} {
		if _, ok := pairs[p]; ok {
			t.Errorf("pair %v flagged but should not be: %v", p, got)
		}
	}
	if len(got) != 2 {
		t.Errorf("pairs = %v, want exactly 2", got)
	}
}
