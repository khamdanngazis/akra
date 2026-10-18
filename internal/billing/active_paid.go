package billing

// Fungsi di file ini murni: tidak membaca database, context, atau jam
// sistem (sama dengan status.go).

// VoidApproved adalah status pembatalan pembayaran yang sudah berlaku
// (payment_voids.status). Status lain (pending, rejected) tidak membatalkan
// pembayaran.
const VoidApproved = "approved"

// PaymentLine adalah satu pembayaran tagihan: id dan nominalnya.
type PaymentLine struct {
	ID     string
	Amount int64
}

// ActivePaid menjumlahkan nominal pembayaran yang masih aktif, yaitu yang
// tidak punya pembatalan berstatus approved. voids memetakan id pembayaran
// ke status pembatalannya; pembatalan yang menunggu atau ditolak tidak
// mengurangi jumlah.
func ActivePaid(payments []PaymentLine, voids map[string]string) int64 {
	var sum int64
	for _, p := range payments {
		if voids[p.ID] == VoidApproved {
			continue
		}
		sum += p.Amount
	}
	return sum
}

// ActivePaidSQL adalah ekspresi SQL jumlah pembayaran aktif tagihan beralias
// i: pembayaran tanpa pembatalan berstatus approved (sama dengan
// ActivePaid). Dipakai halaman pembayaran, daftar tunggakan, dan laporan
// agar angkanya sama persis.
const ActivePaidSQL = `(SELECT coalesce(sum(p.amount), 0) FROM payments p
	WHERE p.school_id = i.school_id AND p.invoice_id = i.id
	  AND NOT EXISTS (SELECT 1 FROM payment_voids v WHERE v.payment_id = p.id AND v.status = 'approved'))`

// FeeNamesSQL adalah nama jenis tagihan item tagihan beralias i, dipisah
// koma sesuai urutan item.
const FeeNamesSQL = `(SELECT string_agg(f.name, ', ' ORDER BY it.position, it.created_at)
	FROM invoice_items it JOIN fee_types f ON f.school_id = it.school_id AND f.id = it.fee_type_id
	WHERE it.school_id = i.school_id AND it.invoice_id = i.id)`
