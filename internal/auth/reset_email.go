package auth

import (
	"fmt"

	"akra/internal/platform/mail"
)

// passwordResetMessage menyusun email berisi tautan reset password.
func passwordResetMessage(u User, link string) mail.Message {
	return mail.Message{
		To:      u.Email,
		Subject: "Atur ulang password akun AKRA",
		Text: fmt.Sprintf(`Halo %s,

Kami menerima permintaan untuk mengatur ulang password akun AKRA Anda di %s.

Buka tautan berikut untuk membuat password baru:
%s

Tautan ini berlaku %d menit dan hanya bisa dipakai sekali.

Jika Anda tidak meminta reset password, abaikan email ini. Password Anda tidak akan berubah.

Salam,
AKRA
`, u.FullName, u.SchoolName, link, int(ResetTokenTTL.Minutes())),
	}
}
