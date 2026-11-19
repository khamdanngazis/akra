package ops

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"akra/internal/exam"
	"akra/internal/platform/database"
)

// MaxEmergency: tanda deploy darurat paling lama berlaku sekian, agar
// variabel yang lupa dihapus tidak membuka jendela deploy selamanya.
const MaxEmergency = 6 * time.Hour

// GuardResult adalah keputusan penjaga deploy.
type GuardResult struct {
	Allowed   bool
	Emergency bool
	Reason    string
	Active    exam.ActiveExams
}

// CheckDeploy memutuskan apakah deploy boleh berjalan: ditolak bila ada
// sesi ujian berlangsung, sesi mulai dalam 30 menit, atau peserta sedang
// mengerjakan, kecuali emergencyUntil (RFC 3339, mis.
// 2026-11-19T12:00:00+07:00) masih berlaku dan tidak lebih dari 6 jam ke
// depan. Database tanpa tabel ujian (deploy pertama) selalu boleh.
func CheckDeploy(ctx context.Context, q database.DBTX, now time.Time, emergencyUntil string) (GuardResult, error) {
	var res GuardResult
	a, err := exam.LoadActiveExams(ctx, q, now, exam.DeployLead)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "42P01" {
		res.Allowed, res.Reason = true, "database belum berisi tabel ujian"
		return res, nil
	}
	if err != nil {
		return res, err
	}
	res.Active = a
	if !a.Blocking() {
		res.Allowed, res.Reason = true, "tidak ada ujian aktif"
		return res, nil
	}
	summary := fmt.Sprintf("%d sesi berlangsung, %d sesi mulai dalam 30 menit, %d peserta mengerjakan", a.Active, a.Upcoming, a.RunningAttempts)
	if s := strings.TrimSpace(emergencyUntil); s != "" {
		until, err := time.Parse(time.RFC3339, s)
		switch {
		case err != nil:
			res.Reason = "DEPLOY_EMERGENCY_UNTIL tidak valid (pakai RFC 3339, mis. 2026-11-19T12:00:00+07:00); " + summary
		case !now.Before(until):
			res.Reason = "DEPLOY_EMERGENCY_UNTIL sudah lewat; " + summary
		case until.Sub(now) > MaxEmergency:
			res.Reason = "DEPLOY_EMERGENCY_UNTIL lebih dari 6 jam ke depan; " + summary
		default:
			res.Allowed, res.Emergency = true, true
			res.Reason = "deploy darurat sampai " + until.Format(time.RFC3339) + "; " + summary
		}
		return res, nil
	}
	res.Reason = summary
	return res, nil
}
