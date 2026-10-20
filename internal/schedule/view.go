package schedule

import (
	"context"
	"time"

	"akra/internal/platform/clock"
	"akra/internal/platform/database"
)

// DayView adalah slot satu hari untuk tampilan mingguan.
type DayView struct {
	Day   int
	Name  string
	Short string // "Sen", untuk tab hari di layar sempit
	Slots []Slot
}

// GroupByDay mengelompokkan slot (sudah urut hari dan jam) per hari Senin–Sabtu.
func GroupByDay(slots []Slot) []DayView {
	days := make([]DayView, 0, 6)
	for _, d := range Days() {
		dv := DayView{Day: d, Name: DayName(d), Short: DayName(d)[:3]}
		for _, s := range slots {
			if s.Day == d {
				dv.Slots = append(dv.Slots, s)
			}
		}
		days = append(days, dv)
	}
	return days
}

// ClassWeek mengembalikan jadwal kelas classID per hari, urut jam.
func ClassWeek(ctx context.Context, q database.DBTX, schoolID, classID string) ([]DayView, error) {
	slots, err := ListClassSlots(ctx, q, schoolID, classID)
	if err != nil {
		return nil, err
	}
	return GroupByDay(slots), nil
}

// TeacherWeek mengembalikan jadwal mengajar guru teacherID di tahun ajaran
// aktif, per hari, urut jam.
func TeacherWeek(ctx context.Context, q database.DBTX, schoolID, teacherID string) ([]DayView, error) {
	if !database.IsUUID(teacherID) {
		return GroupByDay(nil), nil
	}
	slots, err := listSlots(ctx, q, slotSelect+`
		JOIN academic_years y ON y.school_id = sl.school_id AND y.id = sl.academic_year_id AND y.is_active
		WHERE sl.school_id = $1 AND sl.teacher_id = $2
		ORDER BY sl.day_of_week, sl.start_time, c.name`, schoolID, teacherID)
	if err != nil {
		return nil, err
	}
	return GroupByDay(slots), nil
}

// TodayTab mengembalikan hari jadwal untuk t di Asia/Jakarta, untuk tab yang
// dibuka pertama di layar sempit. Minggu dibuka sebagai Senin.
func TodayTab(t time.Time) int {
	d := int(t.In(clock.Jakarta).Weekday())
	if d == 0 {
		return 1
	}
	return d
}

// WeekView adalah data partial "schedule_week".
type WeekView struct {
	Days        []DayView
	Today       int
	ShowClass   bool // tampilkan nama kelas (jadwal guru)
	ShowTeacher bool // tampilkan nama guru (jadwal kelas)
	CanEdit     bool // tautan ubah per slot
}

// NonEmpty melaporkan apakah ada slot di minggu ini.
func (w WeekView) NonEmpty() bool {
	for _, d := range w.Days {
		if len(d.Slots) > 0 {
			return true
		}
	}
	return false
}
