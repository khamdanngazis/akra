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
