package builder

import "akra/internal/billing"

// Sumber awal laporan kustom. Semua SQL di file ini ditulis tangan dan tetap;
// tidak ada bagian yang berasal dari pengguna.
//
// Aturan menambah sumber atau kolom:
//   - Hanya kolom yang aman ditampilkan. Jangan sertakan catatan bebas,
//     alamat, telepon, rekening, atau kunci storage (data pribadi).
//   - Nominal selalu bigint rupiah (TypeMoney). Nilai ujian dalam perseratus
//     (85,5 = 8550) sebagai TypeInt, sama dengan grades.Score. Tidak ada
//     pecahan atau float.
//   - Nilai akhir mapel TIDAK ada di sini: ia dihitung di Go dengan
//     grades.FinalScore (T71), bukan di SQL.
//   - Sumber baris-per-baris; tidak ada agregasi (GROUP BY), supaya filter
//     kolom tetap berarti sama di WHERE.
//   - Setiap JOIN ikut mencocokkan school_id (konvensi proyek), meski foreign
//     key komposit sudah menjamin data satu sekolah.
//   - Nama tabel dan kolom dicek terhadap skema sungguhan oleh
//     sources_db_test.go (setiap sumber dijalankan dengan semua kolomnya).

// "Hari ini" menurut Asia/Jakarta, sama dengan clock.Today().
const todayWIB = "(now() AT TIME ZONE 'Asia/Jakarta')::date"

// studentColumns mengembalikan kolom siswa dan kelas yang dipakai banyak sumber.
func studentColumns() []Column {
	return []Column{
		{Key: "nis", Label: "NIS", Type: TypeText, Expr: "st.nis"},
		{Key: "student_name", Label: "Nama siswa", Type: TypeText, Expr: "st.full_name"},
		{Key: "class_name", Label: "Kelas", Type: TypeText, Expr: "c.name"},
	}
}

func join(groups ...[]Column) []Column {
	var out []Column
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// Default adalah registry bawaan aplikasi.
var Default = mustRegistry()

func mustRegistry() *Registry {
	r, err := NewRegistry(
		attendanceSource(),
		arrearsSource(),
		paymentsSource(),
		gradesSource(),
	)
	if err != nil {
		panic(err) // kesalahan pemrograman; tertangkap oleh uji saat build
	}
	return r
}

// attendance: satu baris per catatan absensi (T27). Hanya catatan pada
// tanggal di dalam masa enrollment siswa, sama dengan aturan rekap.
func attendanceSource() Source {
	return Source{
		Name:  "attendance",
		Label: "Absensi",
		From: `attendance_records ar
 JOIN attendance_sessions asn ON asn.school_id = ar.school_id AND asn.id = ar.session_id
 JOIN enrollments e ON e.school_id = ar.school_id AND e.id = ar.enrollment_id
 JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
 JOIN classes c ON c.school_id = asn.school_id AND c.id = asn.class_id
 LEFT JOIN subjects sub ON sub.school_id = asn.school_id AND sub.id = asn.subject_id`,
		SchoolExpr: "asn.school_id",
		Where: `asn.session_date >= e.start_date
 AND (e.end_date IS NULL OR asn.session_date <= e.end_date)`,
		// attendance_records tidak punya kolom id; kuncinya (session_id, enrollment_id).
		DefaultOrder: "c.name, st.full_name, asn.session_date, ar.session_id, ar.enrollment_id",
		Columns: join(studentColumns(), []Column{
			{Key: "session_date", Label: "Tanggal", Type: TypeDate, Expr: "asn.session_date"},
			{Key: "subject", Label: "Sesi", Type: TypeText, Expr: "COALESCE(sub.name, 'Harian')"},
			{Key: "status", Label: "Status", Type: TypeText, Expr: "ar.status"},
		}),
	}
}

// arrears: satu baris per tagihan yang belum lunas, tidak dibatalkan, dan
// lewat jatuh tempo (T42, sama dengan billing.ListArrears). Pembayaran aktif
// dan nama jenis tagihan memakai billing.ActivePaidSQL dan
// billing.FeeNamesSQL, sehingga angkanya sama dengan /keuangan/tunggakan.
func arrearsSource() Source {
	return Source{
		Name:  "arrears",
		Label: "Tunggakan",
		From: `invoices i
 JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
 JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
 JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id
 LEFT JOIN LATERAL (SELECT ` + billing.ActivePaidSQL + `::bigint AS paid) pd ON true`,
		SchoolExpr:   "i.school_id",
		Where:        "i.status <> 'lunas' AND i.voided_at IS NULL AND i.due_date < " + todayWIB,
		DefaultOrder: "c.name, st.full_name, i.due_date, i.id",
		Columns: join(studentColumns(), []Column{
			{Key: "fee_types", Label: "Jenis tagihan", Type: TypeText, Expr: "COALESCE(" + billing.FeeNamesSQL + ", '')"},
			{Key: "period", Label: "Periode", Type: TypeText, Expr: "i.period"},
			{Key: "due_date", Label: "Jatuh tempo", Type: TypeDate, Expr: "i.due_date"},
			{Key: "total", Label: "Total", Type: TypeMoney, Expr: "i.total_amount"},
			{Key: "paid", Label: "Sudah dibayar", Type: TypeMoney, Expr: "pd.paid"},
			{Key: "remaining", Label: "Sisa", Type: TypeMoney, Expr: "(i.total_amount - pd.paid)"},
			{Key: "days_overdue", Label: "Hari terlambat", Type: TypeInt, Expr: "(" + todayWIB + " - i.due_date)"},
		}),
	}
}

// payments: satu baris per pembayaran (T34), termasuk yang dibatalkan
// (kolom status). Catatan, rekening tujuan, nomor referensi, dan bukti
// sengaja tidak tersedia.
func paymentsSource() Source {
	return Source{
		Name:  "payments",
		Label: "Pembayaran",
		From: `payments p
 JOIN invoices i ON i.school_id = p.school_id AND i.id = p.invoice_id
 JOIN enrollments e ON e.school_id = i.school_id AND e.id = i.enrollment_id
 JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
 JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id`,
		SchoolExpr:   "p.school_id",
		DefaultOrder: "p.paid_on DESC, p.receipt_number",
		Columns: join(studentColumns(), []Column{
			{Key: "receipt_number", Label: "No. kuitansi", Type: TypeText, Expr: "p.receipt_number"},
			{Key: "payment_date", Label: "Tanggal bayar", Type: TypeDate, Expr: "p.paid_on"},
			{Key: "period", Label: "Periode", Type: TypeText, Expr: "i.period"},
			{Key: "amount", Label: "Nominal", Type: TypeMoney, Expr: "p.amount"},
			{Key: "method", Label: "Metode", Type: TypeText, Expr: "p.method"},
			{Key: "status", Label: "Status", Type: TypeText,
				Expr: "CASE WHEN EXISTS (SELECT 1 FROM payment_voids v WHERE v.school_id = p.school_id AND v.payment_id = p.id AND v.status = 'approved') THEN 'dibatalkan' ELSE 'aktif' END"},
		}),
	}
}

// grades: satu baris per nilai komponen yang sudah diisi (T72). NULL
// (belum dinilai) tidak ikut. Nilai dalam perseratus.
func gradesSource() Source {
	return Source{
		Name:  "grades",
		Label: "Nilai",
		From: `grades g
 JOIN grade_components gc ON gc.school_id = g.school_id AND gc.id = g.component_id
 JOIN subjects sub ON sub.school_id = gc.school_id AND sub.id = gc.subject_id
 JOIN enrollments e ON e.school_id = g.school_id AND e.id = g.enrollment_id
 JOIN students st ON st.school_id = e.school_id AND st.id = e.student_id
 JOIN classes c ON c.school_id = e.school_id AND c.id = e.class_id`,
		SchoolExpr:   "g.school_id",
		Where:        "g.score IS NOT NULL",
		DefaultOrder: "c.name, st.full_name, sub.name, gc.name, g.id",
		Columns: join(studentColumns(), []Column{
			{Key: "subject", Label: "Mata pelajaran", Type: TypeText, Expr: "sub.name"},
			{Key: "component", Label: "Komponen", Type: TypeText, Expr: "gc.name"},
			{Key: "weight", Label: "Bobot (%)", Type: TypeInt, Expr: "gc.weight_percent"},
			{Key: "score_x100", Label: "Nilai (perseratus)", Type: TypeInt, Expr: "(g.score * 100)::int"},
		}),
	}
}
