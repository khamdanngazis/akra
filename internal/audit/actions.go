package audit

// Aksi keuangan yang wajib diaudit (T40). Paket billing dan payments memakai
// konstanta ini; halaman audit keuangan memfilter dengan FinanceActions.
const (
	ActionFeeTypeCreate     = "fee_type.create"
	ActionFeeTypeUpdate     = "fee_type.update"
	ActionFeeTypeDeactivate = "fee_type.deactivate"
	ActionFeeTypeActivate   = "fee_type.activate"
	ActionInvoiceCreate     = "invoice.create"
	ActionBulkCreate        = "billing.bulk_create"
	ActionPaymentCreate     = "payment.create"
	ActionProofUpload       = "payment.proof_upload"
	ActionProofView         = "payment.proof_view"
	ActionVoidRequest       = "payment.void_request"
	ActionVoidApprove       = "payment.void_approve"
	ActionVoidReject        = "payment.void_reject"
	ActionSettingsUpdate    = "settings.update"
	// ActionReceiptDownloadParent dicatat portal orang tua (T46).
	ActionReceiptDownloadParent = "receipt.download_parent"
)

// FinanceActions adalah semua aksi keuangan, urut tampil di filter.
var FinanceActions = []string{
	ActionPaymentCreate, ActionVoidRequest, ActionVoidApprove, ActionVoidReject,
	ActionProofUpload, ActionProofView, ActionInvoiceCreate, ActionBulkCreate,
	ActionFeeTypeCreate, ActionFeeTypeUpdate, ActionFeeTypeDeactivate, ActionFeeTypeActivate,
	ActionSettingsUpdate, ActionReceiptDownloadParent,
}

// ActionLabels adalah nama aksi untuk ditampilkan.
var ActionLabels = map[string]string{
	ActionFeeTypeCreate:         "Jenis tagihan dibuat",
	ActionFeeTypeUpdate:         "Jenis tagihan diubah",
	ActionFeeTypeDeactivate:     "Jenis tagihan dinonaktifkan",
	ActionFeeTypeActivate:       "Jenis tagihan diaktifkan",
	ActionInvoiceCreate:         "Tagihan dibuat",
	ActionBulkCreate:            "Tagihan massal",
	ActionPaymentCreate:         "Pembayaran dicatat",
	ActionProofUpload:           "Bukti transfer diunggah",
	ActionProofView:             "Bukti transfer dibuka",
	ActionVoidRequest:           "Pembatalan diajukan",
	ActionVoidApprove:           "Pembatalan disetujui",
	ActionVoidReject:            "Pembatalan ditolak",
	ActionSettingsUpdate:        "Pengaturan keuangan diubah",
	ActionReceiptDownloadParent: "Kuitansi diunduh orang tua",
}
