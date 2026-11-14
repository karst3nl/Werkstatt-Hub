// Package web contains the local, server-rendered HTTP application.
package web

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/ideas"
	"werkstatt-hub/internal/storage"
)

//go:embed templates/*.html assets/*
var files embed.FS

const sessionCookie = "werkstatt_session"
const csrfCookie = "werkstatt_csrf"

type Options struct {
	SecureCookies bool
	SessionTTL    time.Duration
}

type session struct {
	UserID  int64
	Expires time.Time
}

// nameSessions deliberately isolates the first-version name selection from
// authorization. PIN/password authentication can issue the same opaque session.
type nameSessions struct {
	mu    sync.Mutex
	items map[string]session
	ttl   time.Duration
}

type app struct {
	store     *storage.Store
	options   Options
	templates *template.Template
	sessions  nameSessions
}

type page struct {
	Title                 string
	Current               *core.User
	CSRF                  string
	AdminLogin            bool
	Users                 []core.User
	Entries               []core.Entry
	Entry                 core.Entry
	Comments              []core.Comment
	Events                []core.AuditEvent
	CanEdit               bool
	CanManage             bool
	Editing               bool
	IdeaMode              bool
	Category              string
	BasePath              string
	Ideas                 []ideas.Idea
	Categories            []string
	Filter                ideas.Filter
	FilterEmployee        string
	FilterSupervisor      string
	Scale                 core.PriorityScale
	CanEmployeePriority   bool
	CanSupervisorPriority bool
}

// New builds a handler with no external assets or services.
func New(store *storage.Store, options Options) (http.Handler, error) {
	if options.SessionTTL <= 0 {
		options.SessionTTL = 12 * time.Hour
	}
	t, err := template.New("app").Funcs(template.FuncMap{
		"date":             func(t time.Time) string { return t.UTC().Format("02.01.2006 15:04:05 UTC") },
		"role":             roleLabel,
		"status":           statusLabel,
		"admin":            core.CanAdmin,
		"manager":          core.CanManage,
		"action":           auditAction,
		"entity":           auditEntity,
		"priority":         priorityValue,
		"prioritySelected": prioritySelected,
		"diverges":         store.PriorityScale().Diverges,
	}).ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, fmt.Errorf("templates: %w", err)
	}
	a := &app{store: store, options: options, templates: t, sessions: nameSessions{items: make(map[string]session), ttl: options.SessionTTL}}
	mux := http.NewServeMux()
	assets, err := fs.Sub(files, "assets")
	if err != nil {
		return nil, err
	}
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServer(http.FS(assets))))
	mux.HandleFunc("GET /login", a.login(false))
	mux.HandleFunc("POST /login", a.login(false))
	mux.HandleFunc("GET /admin/login", a.login(true))
	mux.HandleFunc("POST /admin/login", a.login(true))
	mux.HandleFunc("POST /logout", a.auth(a.logout))
	mux.HandleFunc("GET /{$}", a.auth(a.home))
	mux.HandleFunc("GET /entries/new", a.auth(a.newEntry))
	mux.HandleFunc("POST /entries/new", a.auth(a.createEntry))
	mux.HandleFunc("GET /entries/{id}", a.auth(a.entry))
	mux.HandleFunc("GET /entries/{id}/edit", a.auth(a.editEntry))
	mux.HandleFunc("POST /entries/{id}/edit", a.auth(a.updateEntry))
	mux.HandleFunc("POST /entries/{id}/comments", a.auth(a.comment))
	mux.HandleFunc("POST /entries/{id}/priority", a.auth(a.setPriority))
	mux.HandleFunc("GET /ideas", a.auth(a.ideaBoard))
	mux.HandleFunc("GET /ideas/new", a.auth(a.newIdea))
	mux.HandleFunc("POST /ideas/new", a.auth(a.createIdea))
	mux.HandleFunc("GET /ideas/{id}", a.auth(a.idea))
	mux.HandleFunc("GET /ideas/{id}/edit", a.auth(a.editIdea))
	mux.HandleFunc("POST /ideas/{id}/edit", a.auth(a.updateIdea))
	mux.HandleFunc("POST /ideas/{id}/comments", a.auth(a.ideaComment))
	mux.HandleFunc("POST /ideas/{id}/priority", a.auth(a.setPriority))
	mux.HandleFunc("GET /audit", a.auth(a.audit))
	mux.HandleFunc("GET /admin/users", a.auth(a.users))
	mux.HandleFunc("POST /admin/users", a.auth(a.createUser))
	mux.HandleFunc("POST /admin/users/{id}", a.auth(a.updateUser))
	return a.protect(mux), nil
}

func roleLabel(role core.Role) string {
	switch role {
	case core.RoleEmployee:
		return "Mitarbeiter"
	case core.RoleSupervisor:
		return "Vorgesetzter"
	case core.RoleAdmin:
		return "Admin"
	default:
		return string(role)
	}
}

func statusLabel(status string) string {
	switch status {
	case "neu":
		return "Neu"
	case "in_pruefung":
		return "In Prüfung"
	case "geplant":
		return "Geplant"
	case "erledigt":
		return "Erledigt"
	case "umgesetzt":
		return "Umgesetzt"
	case "abgelehnt":
		return "Abgelehnt"
	default:
		return status
	}
}

func auditAction(action string) string {
	switch action {
	case "entry.create":
		return "Eintrag erstellt"
	case "entry.update":
		return "Eintrag geändert"
	case "entry.priority":
		return "Eintragspriorität geändert"
	case "idea.create":
		return "Idee erstellt"
	case "idea.update":
		return "Idee geändert"
	case "idea.priority":
		return "Ideenpriorität geändert"
	case "comment.create":
		return "Kommentar erstellt"
	case "user.create":
		return "Nutzer angelegt"
	case "user.update":
		return "Nutzer geändert"
	case "bootstrap_admin":
		return "Erster Admin eingerichtet"
	default:
		return action
	}
}

func auditEntity(entity string) string {
	switch entity {
	case "entry":
		return "Eintrag"
	case "idea":
		return "Idee"
	case "user":
		return "Nutzer"
	case "comment":
		return "Kommentar"
	default:
		return entity
	}
}

func token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func validToken(t string) bool {
	if len(t) != 64 {
		return false
	}
	_, err := hex.DecodeString(t)
	return err == nil
}

func (a *app) cookie(w http.ResponseWriter, name, value string, ttl time.Duration) {
	c := &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: a.options.SecureCookies, SameSite: http.SameSiteStrictMode}
	if ttl < 0 {
		c.MaxAge = -1
		c.Expires = time.Unix(1, 0)
	} else if ttl > 0 {
		c.Expires = time.Now().Add(ttl)
		c.MaxAge = int(ttl.Seconds())
		if c.MaxAge < 1 {
			c.MaxAge = 1
		}
	}
	http.SetCookie(w, c)
}

type csrfKey struct{}

func (a *app) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'none'; style-src 'self'; img-src 'self'; font-src 'self'; connect-src 'none'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		// Preserve same-origin form origins. With no-referrer, Chromium sends
		// Origin: null on ordinary POSTs, preventing legitimate submissions.
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Cache-Control", "no-store")
		csrf := ""
		if c, err := r.Cookie(csrfCookie); err == nil && validToken(c.Value) {
			csrf = c.Value
		}
		if r.Method == http.MethodPost {
			// Form URL encoding can use twelve bytes per Unicode code point.
			// Keep the transport limit above the 20,000-character text limit.
			r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
			if err := r.ParseForm(); err != nil {
				http.Error(w, "Formular ist ungültig oder zu groß.", http.StatusBadRequest)
				return
			}
			provided := r.PostForm.Get("csrf")
			if csrf == "" || !validToken(provided) || subtle.ConstantTimeCompare([]byte(csrf), []byte(provided)) != 1 {
				http.Error(w, "Formular abgelaufen. Bitte Seite neu laden.", http.StatusForbidden)
				return
			}
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				scheme := "http"
				if r.TLS != nil {
					scheme = "https"
				}
				if err != nil || u.Scheme != scheme || u.Host != r.Host || u.Path != "" || u.User != nil {
					http.Error(w, "Ungültiger Formularursprung.", http.StatusForbidden)
					return
				}
			}
		} else if csrf == "" && !strings.HasPrefix(r.URL.Path, "/assets/") {
			var err error
			csrf, err = token()
			if err != nil {
				a.fail(w, err)
				return
			}
			a.cookie(w, csrfCookie, csrf, 0)
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey{}, csrf)))
	})
}

func (a *app) render(w http.ResponseWriter, r *http.Request, name string, p page) {
	p.CSRF, _ = r.Context().Value(csrfKey{}).(string)
	p.Scale = a.store.PriorityScale()
	p.BasePath = "/entries"
	if p.IdeaMode {
		p.BasePath = "/ideas"
	}
	if p.Current != nil {
		p.CanEmployeePriority = core.CanSetEmployeePriority(*p.Current, p.Entry)
		p.CanSupervisorPriority = core.CanSetSupervisorPriority(*p.Current)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.templates.ExecuteTemplate(w, name, p); err != nil {
		log.Printf("render template %s: %v", name, err)
	}
}

func (a *app) fail(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, core.ErrForbidden):
		http.Error(w, "Keine Berechtigung für diese Aktion.", http.StatusForbidden)
	case errors.Is(err, core.ErrNotFound):
		http.Error(w, "Nicht gefunden.", http.StatusNotFound)
	case errors.Is(err, core.ErrValidation):
		http.Error(w, "Bitte gültige Angaben verwenden. Name, Titel und Kategorie dürfen nicht leer sein. Texte und Prioritäten müssen innerhalb der angezeigten Grenzen liegen.", http.StatusBadRequest)
	case errors.Is(err, core.ErrConflict):
		http.Error(w, "Der Eintrag wurde inzwischen geändert oder die Änderung ist nicht möglich. Bitte Seite neu laden.", http.StatusConflict)
	default:
		log.Printf("request failed: %v", err)
		http.Error(w, "Die Anfrage konnte nicht verarbeitet werden.", http.StatusInternalServerError)
	}
}

func (a *app) current(r *http.Request) (core.User, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil || !validToken(c.Value) {
		return core.User{}, core.ErrForbidden
	}
	a.sessions.mu.Lock()
	s, ok := a.sessions.items[c.Value]
	if ok && !s.Expires.After(time.Now()) {
		delete(a.sessions.items, c.Value)
		ok = false
	}
	a.sessions.mu.Unlock()
	if !ok {
		return core.User{}, core.ErrForbidden
	}
	u, err := a.store.User(r.Context(), s.UserID)
	if err != nil {
		return core.User{}, err
	}
	if !u.Active {
		a.sessions.mu.Lock()
		delete(a.sessions.items, c.Value)
		a.sessions.mu.Unlock()
		return core.User{}, core.ErrForbidden
	}
	return u, nil
}

func (a *app) auth(next func(http.ResponseWriter, *http.Request, core.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := a.current(r)
		if errors.Is(err, core.ErrForbidden) || errors.Is(err, core.ErrNotFound) {
			a.cookie(w, sessionCookie, "", -1)
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		if err != nil {
			a.fail(w, err)
			return
		}
		next(w, r, u)
	}
}

func (a *app) login(admin bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		users, err := a.store.Users(r.Context())
		if err != nil {
			a.fail(w, err)
			return
		}
		allowed := make([]core.User, 0, len(users))
		for _, u := range users {
			if u.Active && (u.Role == core.RoleAdmin) == admin {
				allowed = append(allowed, u)
			}
		}
		if r.Method == http.MethodGet {
			a.render(w, r, "login", page{Title: "Namen auswählen", Users: allowed, AdminLogin: admin})
			return
		}
		id, err := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
		if err != nil {
			http.Error(w, "Bitte einen Namen auswählen.", http.StatusBadRequest)
			return
		}
		var selected *core.User
		for _, u := range allowed {
			if u.ID == id {
				v := u
				selected = &v
				break
			}
		}
		if selected == nil {
			a.fail(w, core.ErrForbidden)
			return
		}
		key, err := token()
		if err != nil {
			a.fail(w, err)
			return
		}
		a.sessions.mu.Lock()
		if c, err := r.Cookie(sessionCookie); err == nil {
			delete(a.sessions.items, c.Value)
		}
		now := time.Now()
		for k, s := range a.sessions.items {
			if !s.Expires.After(now) {
				delete(a.sessions.items, k)
			}
		}
		a.sessions.items[key] = session{UserID: selected.ID, Expires: now.Add(a.sessions.ttl)}
		a.sessions.mu.Unlock()
		a.cookie(w, sessionCookie, key, a.sessions.ttl)
		target := "/"
		if admin {
			target = "/admin/users"
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}

func (a *app) logout(w http.ResponseWriter, r *http.Request, u core.User) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.sessions.mu.Lock()
		delete(a.sessions.items, c.Value)
		a.sessions.mu.Unlock()
	}
	a.cookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (a *app) home(w http.ResponseWriter, r *http.Request, u core.User) {
	entries, err := a.store.Entries(r.Context(), u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	latestIdeas, err := a.store.Ideas(r.Context(), u.ID, ideas.Filter{})
	if err != nil {
		a.fail(w, err)
		return
	}
	if len(latestIdeas) > 3 {
		latestIdeas = latestIdeas[:3]
	}
	a.render(w, r, "home", page{Title: "Werkstatt-Hub", Current: &u, Entries: entries, Ideas: latestIdeas})
}

func (a *app) newEntry(w http.ResponseWriter, r *http.Request, u core.User) {
	a.render(w, r, "form", page{Title: "Neuer Eintrag", Current: &u})
}

func (a *app) createEntry(w http.ResponseWriter, r *http.Request, u core.User) {
	e, err := a.store.CreateEntry(r.Context(), u.ID, core.EntryInput{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body")})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/entries/%d", e.ID), http.StatusSeeOther)
}

func entryID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, core.ErrNotFound
	}
	return id, nil
}

func (a *app) entry(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	e, err := a.store.Entry(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if e.Module == "idee" {
		http.Redirect(w, r, fmt.Sprintf("/ideas/%d", id), http.StatusSeeOther)
		return
	}
	comments, err := a.store.Comments(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "entry", page{Title: e.Title, Current: &u, Entry: e, Comments: comments, CanEdit: core.CanEditEntry(u, e), CanManage: core.CanManage(u)})
}

func (a *app) editEntry(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	e, err := a.store.Entry(r.Context(), u.ID, id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if e.Module == "idee" {
		http.Redirect(w, r, fmt.Sprintf("/ideas/%d/edit", id), http.StatusSeeOther)
		return
	}
	if !core.CanEditEntry(u, e) {
		a.fail(w, core.ErrForbidden)
		return
	}
	a.render(w, r, "form", page{Title: "Eintrag bearbeiten", Current: &u, Entry: e, Editing: true, CanManage: core.CanManage(u)})
}

func (a *app) updateEntry(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	version, err := strconv.ParseInt(r.PostForm.Get("version"), 10, 64)
	if err != nil {
		a.fail(w, core.ErrValidation)
		return
	}
	// The current status is retained for employees; the store still independently
	// checks status permissions, including forged requests.
	status := r.PostForm.Get("status")
	if !core.CanManage(u) && status == "" {
		e, err := a.store.Entry(r.Context(), u.ID, id)
		if err != nil {
			a.fail(w, err)
			return
		}
		status = e.Status
	}
	err = a.store.UpdateEntry(r.Context(), u.ID, id, core.EntryUpdate{Title: r.PostForm.Get("title"), Body: r.PostForm.Get("body"), Status: status, Version: version})
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/entries/%d", id), http.StatusSeeOther)
}

func (a *app) comment(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	if err = a.store.AddComment(r.Context(), u.ID, id, r.PostForm.Get("body")); err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/entries/%d", id), http.StatusSeeOther)
}

func (a *app) audit(w http.ResponseWriter, r *http.Request, u core.User) {
	events, err := a.store.Audit(r.Context(), u.ID)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "audit", page{Title: "Änderungsprotokoll", Current: &u, Events: events})
}

func (a *app) users(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanAdmin(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	users, err := a.store.Users(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "users", page{Title: "Benutzerverwaltung", Current: &u, Users: users})
}

func (a *app) createUser(w http.ResponseWriter, r *http.Request, u core.User) {
	_, err := a.store.CreateUser(r.Context(), u.ID, r.PostForm.Get("name"), core.Role(r.PostForm.Get("role")))
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (a *app) updateUser(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	err = a.store.UpdateUser(r.Context(), u.ID, id, r.PostForm.Get("name"), core.Role(r.PostForm.Get("role")), r.PostForm.Get("active") == "1")
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
