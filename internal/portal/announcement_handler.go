package portal

import (
	"net/http"

	"akra/internal/announcements"
	"akra/internal/auth"
	"akra/internal/platform/page"
)

const pageAnnouncements = "portal/announcements"

type announcementItem struct {
	Title, Body, Audience, Published string
}

type announcementsView struct {
	User  auth.Identity
	Items []announcementItem
	Flash string
	Error string
}

// announcementsPage menampilkan pengumuman untuk semua kelas dan untuk
// kelas anak-anak akun yang login, di tahun ajaran aktif.
func (h *Handler) announcementsPage(w http.ResponseWriter, r *http.Request) {
	id, _ := auth.CurrentUser(r.Context())
	list, err := announcements.ListForParent(r.Context(), h.svc.db, id.SchoolID, id.UserID, 50)
	if err != nil {
		page.ServerError(h.render, h.logger, w, r, id, "parent announcements", err)
		return
	}
	v := announcementsView{User: id}
	for _, a := range list {
		v.Items = append(v.Items, announcementItem{Title: a.Title, Body: a.Body, Audience: announcements.Audience(a), Published: announcements.FormatPublished(a)})
	}
	h.render.Render(w, r, http.StatusOK, pageAnnouncements, v)
}
