package web

import (
	"errors"
	"net/http"

	"werkstatt-hub/internal/core"
)

func (a *app) renderStatus(w http.ResponseWriter, r *http.Request, name string, p page, status int) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	a.render(w, r, name, p)
}

func (a *app) adminPassword(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanAdmin(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	state, err := a.store.AdminAccess(r.Context(), u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	view := page{Title: "Admin-Passwort ändern", Current: &u, PasswordRequired: state.MustChange}
	if r.Method == http.MethodGet {
		a.render(w, r, "admin-password", view)
		return
	}
	if r.PostForm.Get("new_password") != r.PostForm.Get("confirm_password") {
		view.Error = "Die neuen Passwörter stimmen nicht überein."
		a.renderStatus(w, r, "admin-password", view, http.StatusBadRequest)
		return
	}
	fresh, err := a.store.ChangeAdminPassword(r.Context(), u.ID, r.PostForm.Get("current_password"), r.PostForm.Get("new_password"))
	if err != nil {
		if errors.Is(err, core.ErrForbidden) || errors.Is(err, core.ErrValidation) {
			view.Error = "Bitte das aktuelle Passwort prüfen und ein anderes neues Passwort mit 12 bis 128 Zeichen verwenden."
			a.renderStatus(w, r, "admin-password", view, http.StatusBadRequest)
			return
		}
		a.fail(w, err)
		return
	}
	if err := a.issueSession(w, r, fresh.User, true); err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
func (a *app) deleteUser(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanAdmin(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if err = a.store.DeleteUser(r.Context(), u.ID, id); err != nil {
		if errors.Is(err, core.ErrValidation) {
			http.Error(w, "Der letzte aktive Admin kann nicht gelöscht werden.", http.StatusBadRequest)
			return
		}
		a.fail(w, err)
		return
	}
	if id == u.ID {
		a.discardSession(r)
		a.cookie(w, sessionCookie, "", -1)
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
