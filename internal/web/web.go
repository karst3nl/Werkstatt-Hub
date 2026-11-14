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
	"werkstatt-hub/internal/tasks"
	"werkstatt-hub/internal/tooling"
	"werkstatt-hub/internal/wartung"
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
	UserID             int64
	Expires            time.Time
	AdminAuthenticated bool
	AuthVersion        int64
}

// nameSessions deliberately isolates the first-version name selection from
// authorization. PIN/password authentication can issue the same opaque session.
type nameSessions struct {
	mu    sync.Mutex
	items map[string]session
	ttl   time.Duration
}

type app struct {
	store       *storage.Store
	options     Options
	templates   *template.Template
	sessions    nameSessions
	adminLogins loginLimiter
}

type page struct {
	Wartung              wartungCard
	WartungObjects       []wartungCard
	WartungRecords       []wartung.Record
	WartungFilter        wartung.Filter
	WartungSettings      wartung.Settings
	WartungToday         string
	WartungWarningValue  string
	WartungOverdue       []wartungCard
	WartungOverdueCount  int
	WartungSoonCount     int
	Error                string
	PasswordRequired     bool
	Dashboard            bool
	ReportMode           bool
	PurchaseMode         bool
	Tool                 tooling.Tool
	Tools                []tooling.Tool
	ToolFilter           tooling.ToolFilter
	Report               tooling.Report
	Reports              []tooling.Report
	ReportFilter         tooling.ReportFilter
	Purchase             tooling.Purchase
	Purchases            []tooling.Purchase
	PurchaseFilter       tooling.PurchaseFilter
	PriorityFilterPrefix string
	SortFilter           string
	Divergent            bool
	DashboardTools       []tooling.Tool
	DashboardPurchases   []tooling.Purchase
	ToolAttentionCount   int

	TaskMode              bool
	Task                  tasks.Task
	TaskColumns           []taskColumn
	TaskFilter            tasks.Filter
	Mine                  bool
	MyTasks               bool
	Assignees             []core.User
	AssigneeMissing       bool
	TaskStatuses          []taskStatusOption
	CanApprove            bool
	TaskReminder          taskCard
	Reminders             []taskCard
	DashboardTasks        []taskCard
	WorkshopTimezone      string
	Title                 string
	Current               *core.User
	CSRF                  string
	AdminLogin            bool
	Authors               []core.User
	Users                 []core.User
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
		"taskDate":         taskDate,
		"condition":        conditionLabel,
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
		"wrenchRating":     priorityRenderer(store.PriorityScale()),
		"wartungUnit":      wartungUnitLabel,
		"wartungFinding":   wartungFindingLabel,
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
	mux.HandleFunc("POST /logout", a.authIdentity(a.logout))
	mux.HandleFunc("GET /admin/password", a.authIdentity(a.adminPassword))
	mux.HandleFunc("POST /admin/password", a.authIdentity(a.adminPassword))
	mux.HandleFunc("GET /{$}", a.browse(a.home))
	mux.HandleFunc("GET /ideas", a.browse(a.ideaBoard))
	mux.HandleFunc("GET /ideas/new", a.browse(a.newIdea))
	mux.HandleFunc("POST /ideas/new", a.browse(a.createIdea))
	mux.HandleFunc("GET /ideas/{id}", a.browse(a.idea))
	mux.HandleFunc("GET /ideas/{id}/edit", a.auth(a.editIdea))
	mux.HandleFunc("POST /ideas/{id}/edit", a.auth(a.updateIdea))
	mux.HandleFunc("POST /ideas/{id}/comments", a.auth(a.ideaComment))
	mux.HandleFunc("POST /ideas/{id}/priority", a.auth(a.setPriority))
	mux.HandleFunc("GET /tasks", a.browse(a.taskBoard))
	mux.HandleFunc("GET /tasks/new", a.auth(a.newTask))
	mux.HandleFunc("POST /tasks/new", a.auth(a.createTask))
	mux.HandleFunc("GET /tasks/{id}", a.browse(a.task))
	mux.HandleFunc("GET /tasks/{id}/edit", a.auth(a.editTask))
	mux.HandleFunc("POST /tasks/{id}/edit", a.auth(a.updateTask))
	mux.HandleFunc("POST /tasks/{id}/status", a.auth(a.taskStatus))
	mux.HandleFunc("POST /tasks/{id}/comments", a.auth(a.taskComment))
	mux.HandleFunc("POST /tasks/{id}/priority", a.auth(a.setPriority))
	mux.HandleFunc("GET /tools", a.browse(a.toolBoard))
	mux.HandleFunc("GET /tools/new", a.auth(a.newTool))
	mux.HandleFunc("POST /tools/new", a.auth(a.createTool))
	mux.HandleFunc("GET /tools/{id}", a.browse(a.tool))
	mux.HandleFunc("GET /tools/{id}/edit", a.auth(a.editTool))
	mux.HandleFunc("POST /tools/{id}/edit", a.auth(a.updateTool))
	mux.HandleFunc("GET /tools/{id}/reports/new", a.browse(a.newToolReport))
	mux.HandleFunc("POST /tools/{id}/reports/new", a.browse(a.createToolReport))
	mux.HandleFunc("GET /tool-reports/{id}", a.browse(a.toolReport))
	mux.HandleFunc("GET /tool-reports/{id}/edit", a.auth(a.editToolReport))
	mux.HandleFunc("POST /tool-reports/{id}/edit", a.auth(a.updateToolReport))
	mux.HandleFunc("POST /tool-reports/{id}/comments", a.auth(a.toolingComment))
	mux.HandleFunc("POST /tool-reports/{id}/priority", a.auth(a.setPriority))
	mux.HandleFunc("GET /purchases", a.browse(a.purchaseBoard))
	mux.HandleFunc("GET /purchases/new", a.browse(a.newPurchase))
	mux.HandleFunc("POST /purchases/new", a.browse(a.createPurchase))
	mux.HandleFunc("GET /purchases/{id}", a.browse(a.purchase))
	mux.HandleFunc("GET /purchases/{id}/edit", a.auth(a.editPurchase))
	mux.HandleFunc("POST /purchases/{id}/edit", a.auth(a.updatePurchase))
	mux.HandleFunc("POST /purchases/{id}/comments", a.auth(a.toolingComment))
	mux.HandleFunc("POST /purchases/{id}/priority", a.auth(a.setPriority))
	mux.HandleFunc("GET /wartung", a.browse(a.wartungBoard))
	mux.HandleFunc("GET /wartung/{id}", a.browse(a.wartungObject))
	mux.HandleFunc("POST /wartung/{id}/record", a.browse(a.recordWartung))
	mux.HandleFunc("POST /wartung/{id}/records/{recordID}/approve", a.auth(a.approveWartung))
	mux.HandleFunc("GET /admin/wartung/new", a.auth(a.newWartungObject))
	mux.HandleFunc("POST /admin/wartung/new", a.auth(a.createWartungObject))
	mux.HandleFunc("GET /admin/wartung/{id}/edit", a.auth(a.editWartungObject))
	mux.HandleFunc("POST /admin/wartung/{id}/edit", a.auth(a.updateWartungObject))
	mux.HandleFunc("GET /admin/wartung/settings", a.auth(a.wartungSettings))
	mux.HandleFunc("POST /admin/wartung/settings", a.auth(a.wartungSettings))
	mux.HandleFunc("GET /audit", a.auth(a.audit))
	mux.HandleFunc("GET /admin/users", a.auth(a.users))
	mux.HandleFunc("POST /admin/users", a.auth(a.createUser))
	mux.HandleFunc("POST /admin/users/{id}", a.auth(a.updateUser))
	mux.HandleFunc("POST /admin/users/{id}/delete", a.auth(a.deleteUser))
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
	case "offen":
		return "Offen"
	case "in_arbeit":
		return "In Arbeit"
	case "zur_kontrolle":
		return "Zur Kontrolle"
	case "gewuenscht":
		return "Gewünscht"
	case "genehmigt":
		return "Genehmigt"
	case "bestellt":
		return "Bestellt"
	case "geliefert":
		return "Geliefert"
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
	case "wartung.object_create":
		return "Wartungsobjekt angelegt"
	case "wartung.object_update":
		return "Wartungsobjekt geändert"
	case "wartung.record_create":
		return "Wartung eingetragen"
	case "wartung.record_approve":
		return "Wartung abgezeichnet"
	case "wartung.settings_update":
		return "Standard-Vorwarnzeit geändert"
	case "tool.create":
		return "Werkzeug angelegt"
	case "tool.update":
		return "Werkzeug geändert"
	case "tool_report.create":
		return "Werkzeugmeldung erfasst"
	case "tool_report.update":
		return "Werkzeugmeldung geändert"
	case "tool_report.priority":
		return "Meldungspriorität geändert"
	case "purchase.create":
		return "Anschaffungswunsch erfasst"
	case "purchase.update":
		return "Anschaffungswunsch geändert"
	case "purchase.priority":
		return "Anschaffungspriorität geändert"
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
	case "task.create":
		return "Aufgabe erstellt"
	case "task.update":
		return "Aufgabe geändert"
	case "task.status":
		return "Aufgabenstatus geändert"
	case "task.approve":
		return "Aufgabe abgenommen"
	case "task.priority":
		return "Aufgabenpriorität geändert"
	case "comment.create":
		return "Kommentar erstellt"
	case "user.create":
		return "Nutzer angelegt"
	case "user.update":
		return "Nutzer geändert"
	case "user.delete":
		return "Nutzer gelöscht"
	case "admin.password_change":
		return "Admin-Passwort geändert"
	case "admin.password_init", "admin.password_initialize":
		return "Admin-Zugang eingerichtet"
	case "admin.password_reset":
		return "Admin-Startpasswort gesetzt"
	case "bootstrap_admin":
		return "Erster Admin eingerichtet"
	default:
		return action
	}
}

func auditEntity(entity string) string {
	switch entity {
	case "wartung_object":
		return "Wartungsobjekt"
	case "wartung_record":
		return "Wartung"
	case "wartung_settings":
		return "Wartungseinstellungen"
	case "tool":
		return "Werkzeug"
	case "tool_report":
		return "Werkzeugmeldung"
	case "purchase":
		return "Anschaffungswunsch"
	case "entry":
		return "Eintrag"
	case "idea":
		return "Idee"
	case "task":
		return "Aufgabe"
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
			// Maintenance forms contain two independent 20,000-character fields.
			r.Body = http.MaxBytesReader(w, r.Body, 512<<10)
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
	// Public views have no current identity until a user explicitly selects one.
	if p.Current != nil && (!p.Current.Active || p.Current.Deleted || !p.Current.Role.Valid()) {
		p.Current = nil
	}
	p.Scale = a.store.PriorityScale()
	p.WorkshopTimezone = a.store.TaskSettings().Location.String()
	p.BasePath = "/entries"
	if p.IdeaMode {
		p.BasePath = "/ideas"
	}
	if p.TaskMode {
		p.BasePath = "/tasks"
	}
	if p.ReportMode {
		p.BasePath = "/tool-reports"
	}
	if p.PurchaseMode {
		p.BasePath = "/purchases"
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

func (a *app) home(w http.ResponseWriter, r *http.Request, u core.User) {
	latestIdeas, err := a.store.PublicIdeas(r.Context(), ideas.Filter{})
	if err != nil {
		a.fail(w, err)
		return
	}
	if len(latestIdeas) > 3 {
		latestIdeas = latestIdeas[:3]
	}
	filter := tasks.Filter{}
	myTasks := u.Active && !core.CanManage(u)
	if myTasks {
		filter.AssigneeID = u.ID
	}
	taskList, err := a.store.PublicTasks(r.Context(), filter)
	if err != nil {
		a.fail(w, err)
		return
	}
	reminders := make([]taskCard, 0)
	previews := make([]taskCard, 0)
	now := time.Now()
	for _, task := range taskList {
		card := a.taskCard(task, now)
		if card.ReminderKind != "" {
			reminders = append(reminders, card)
		}
		if task.Status != "erledigt" && len(previews) < 4 {
			previews = append(previews, card)
		}
	}
	toolList, err := a.store.Tools(r.Context(), tooling.ToolFilter{})
	if err != nil {
		a.fail(w, err)
		return
	}
	attention := make([]tooling.Tool, 0)
	for _, tool := range toolList {
		if tool.Condition == "defekt" || tool.Condition == "verschleisst" || tool.OpenReports > 0 {
			attention = append(attention, tool)
		}
	}
	attentionCount := len(attention)
	if len(attention) > 4 {
		attention = attention[:4]
	}
	purchaseList, err := a.store.Purchases(r.Context(), tooling.PurchaseFilter{Sort: "vorgesetzter"})
	if err != nil {
		a.fail(w, err)
		return
	}
	purchasePreview := make([]tooling.Purchase, 0)
	for _, purchase := range purchaseList {
		if purchase.Status != "geliefert" && len(purchasePreview) < 3 {
			purchasePreview = append(purchasePreview, purchase)
		}
	}
	maintenance, err := a.store.WartungDashboard(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	maintenanceSettings, err := a.store.WartungSettings(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	maintenanceOverdue, err := a.wartungCards(maintenance.Overdue, maintenanceSettings, now)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, r, "home", page{Title: "Dashboard", Dashboard: true, Current: &u, Ideas: latestIdeas, MyTasks: myTasks, Reminders: reminders, DashboardTasks: previews, DashboardTools: attention, ToolAttentionCount: attentionCount, DashboardPurchases: purchasePreview, WartungOverdue: maintenanceOverdue, WartungOverdueCount: maintenance.OverdueCount, WartungSoonCount: maintenance.SoonCount})
}

func entryID(r *http.Request) (int64, error) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		return 0, core.ErrNotFound
	}
	return id, nil
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
	if !core.CanAdmin(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	if core.Role(r.PostForm.Get("role")) == core.RoleAdmin && r.PostForm.Get("temporary_password") == "" {
		a.fail(w, core.ErrValidation)
		return
	}
	_, err := a.store.CreateUser(r.Context(), u.ID, r.PostForm.Get("name"), core.Role(r.PostForm.Get("role")), r.PostForm.Get("temporary_password"))
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}

func (a *app) updateUser(w http.ResponseWriter, r *http.Request, u core.User) {
	if !core.CanAdmin(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	before, err := a.store.User(r.Context(), id)
	if err != nil {
		a.fail(w, err)
		return
	}
	if before.Role != core.RoleAdmin && core.Role(r.PostForm.Get("role")) == core.RoleAdmin && r.PostForm.Get("temporary_password") == "" {
		a.fail(w, core.ErrValidation)
		return
	}
	err = a.store.UpdateUser(r.Context(), u.ID, id, r.PostForm.Get("name"), core.Role(r.PostForm.Get("role")), r.PostForm.Get("active") == "1", r.PostForm.Get("temporary_password"))
	if err != nil {
		a.fail(w, err)
		return
	}
	http.Redirect(w, r, "/admin/users", http.StatusSeeOther)
}
