package web

import (
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"werkstatt-hub/internal/core"
)

type loginAttempt struct {
	Count int
	Since time.Time
}
type loginLimiter struct {
	mu       sync.Mutex
	attempts map[string]loginAttempt
}

func clientKey(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// admit reserves an attempt before credential verification, so concurrent
// submissions cannot each slip past a separate check/failure update.
func (l *loginLimiter) admit(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.attempts == nil {
		l.attempts = make(map[string]loginAttempt)
	}
	for k, attempt := range l.attempts {
		if now.Sub(attempt.Since) >= time.Minute {
			delete(l.attempts, k)
		}
	}
	attempt, exists := l.attempts[key]
	if exists && attempt.Count >= 5 {
		return false
	}
	if !exists {
		if len(l.attempts) >= 1024 {
			for k := range l.attempts {
				delete(l.attempts, k)
				break
			}
		}
		attempt.Since = now
	}
	attempt.Count++
	l.attempts[key] = attempt
	return true
}

func (l *loginLimiter) clear(key string) { l.mu.Lock(); defer l.mu.Unlock(); delete(l.attempts, key) }

func (a *app) discardSession(r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		a.sessions.mu.Lock()
		delete(a.sessions.items, c.Value)
		a.sessions.mu.Unlock()
	}
}
func (a *app) issueSession(w http.ResponseWriter, r *http.Request, u core.User, adminAuthenticated bool) error {
	key, err := token()
	if err != nil {
		return err
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
	a.sessions.items[key] = session{UserID: u.ID, Expires: now.Add(a.sessions.ttl), AdminAuthenticated: adminAuthenticated, AuthVersion: u.AuthVersion}
	a.sessions.mu.Unlock()
	a.cookie(w, sessionCookie, key, a.sessions.ttl)
	return nil
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
	if !u.Active || u.Deleted || !u.Role.Valid() || u.AuthVersion != s.AuthVersion || u.Role == core.RoleAdmin && !s.AdminAuthenticated {
		a.discardSession(r)
		return core.User{}, core.ErrForbidden
	}
	return u, nil
}
func (a *app) adminPending(r *http.Request, u core.User) (bool, error) {
	if u.Role != core.RoleAdmin {
		return false, nil
	}
	state, err := a.store.AdminAccess(r.Context(), u.ID)
	if err != nil {
		return false, err
	}
	return state.MustChange, nil
}
func (a *app) browse(next func(http.ResponseWriter, *http.Request, core.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := a.current(r)
		if errors.Is(err, core.ErrForbidden) || errors.Is(err, core.ErrNotFound) {
			if _, e := r.Cookie(sessionCookie); e == nil {
				a.cookie(w, sessionCookie, "", -1)
			}
			next(w, r, core.User{})
			return
		}
		if err != nil {
			a.fail(w, err)
			return
		}
		pending, err := a.adminPending(r, u)
		if errors.Is(err, core.ErrForbidden) || errors.Is(err, core.ErrNotFound) {
			a.discardSession(r)
			a.cookie(w, sessionCookie, "", -1)
			next(w, r, core.User{})
			return
		}
		if err != nil {
			a.fail(w, err)
			return
		}
		if pending {
			next(w, r, core.User{})
			return
		}
		next(w, r, u)
	}
}

// authIdentity is intentionally limited to password changes and logout. Other
// protected handlers use auth, which also enforces the mandatory first change.
func (a *app) authIdentity(next func(http.ResponseWriter, *http.Request, core.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, err := a.current(r)
		if errors.Is(err, core.ErrForbidden) || errors.Is(err, core.ErrNotFound) {
			a.cookie(w, sessionCookie, "", -1)
			target := "/login"
			if strings.HasPrefix(r.URL.Path, "/admin/") {
				target = "/admin/login"
			}
			http.Redirect(w, r, target, http.StatusSeeOther)
			return
		}
		if err != nil {
			a.fail(w, err)
			return
		}
		next(w, r, u)
	}
}
func (a *app) auth(next func(http.ResponseWriter, *http.Request, core.User)) http.HandlerFunc {
	return a.authIdentity(func(w http.ResponseWriter, r *http.Request, u core.User) {
		pending, err := a.adminPending(r, u)
		if err != nil {
			a.fail(w, err)
			return
		}
		if pending {
			http.Redirect(w, r, "/admin/password", http.StatusSeeOther)
			return
		}
		next(w, r, u)
	})
}
func (a *app) selectedAuthor(r *http.Request) (core.User, error) {
	id, err := strconv.ParseInt(r.PostForm.Get("author_id"), 10, 64)
	if err != nil || id <= 0 {
		return core.User{}, core.ErrValidation
	}
	u, err := a.store.User(r.Context(), id)
	if errors.Is(err, core.ErrNotFound) {
		return core.User{}, core.ErrValidation
	}
	if err != nil {
		return core.User{}, err
	}
	if !u.Active || u.Deleted || !u.Role.Valid() {
		return core.User{}, core.ErrValidation
	}
	return u, nil
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
			if u.Active && !u.Deleted && (u.Role == core.RoleAdmin) == admin {
				allowed = append(allowed, u)
			}
		}
		if r.Method == http.MethodGet {
			if admin {
				if u, e := a.current(r); e == nil && u.Role == core.RoleAdmin {
					if pending, e := a.adminPending(r, u); e == nil && pending {
						http.Redirect(w, r, "/admin/password", http.StatusSeeOther)
						return
					}
				}
			}
			a.render(w, r, "login", page{Title: "Namen auswählen", Users: allowed, AdminLogin: admin})
			return
		}
		key := clientKey(r)
		if admin && !a.adminLogins.admit(key, time.Now()) {
			w.Header().Set("Retry-After", "60")
			a.renderStatus(w, r, "login", page{Title: "Admin-Anmeldung", Users: allowed, AdminLogin: true, Error: "Zu viele Fehlversuche. Bitte in einer Minute erneut versuchen."}, http.StatusTooManyRequests)
			return
		}
		id, parseErr := strconv.ParseInt(r.PostForm.Get("user_id"), 10, 64)
		var selected core.User
		found := false
		for _, u := range allowed {
			if u.ID == id && parseErr == nil {
				selected = u
				found = true
				break
			}
		}
		pending := false
		if admin {
			if found {
				state, e := a.store.AuthenticateAdmin(r.Context(), id, r.PostForm.Get("password"))
				if e == nil {
					selected = state.User
					pending = state.MustChange
				} else if !errors.Is(e, core.ErrForbidden) && !errors.Is(e, core.ErrNotFound) {
					a.fail(w, e)
					return
				} else {
					found = false
				}
			}
			if !found {
				a.renderStatus(w, r, "login", page{Title: "Admin-Anmeldung", Users: allowed, AdminLogin: true, Error: "Name oder Passwort ist ungültig."}, http.StatusForbidden)
				return
			}
			a.adminLogins.clear(key)
		} else if !found {
			a.fail(w, core.ErrForbidden)
			return
		}
		if err := a.issueSession(w, r, selected, admin); err != nil {
			a.fail(w, err)
			return
		}
		target := "/"
		if admin {
			target = "/admin/users"
			if pending {
				target = "/admin/password"
			}
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
	}
}
func (a *app) logout(w http.ResponseWriter, r *http.Request, u core.User) {
	a.discardSession(r)
	a.cookie(w, sessionCookie, "", -1)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
