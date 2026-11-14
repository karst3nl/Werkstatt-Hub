package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"werkstatt-hub/internal/core"
	"werkstatt-hub/internal/wartung"
)

type wartungCard struct {
	wartung.Object
	NextDueDate          string
	State                string
	StateLabel           string
	EffectiveWarningDays int
}

func wartungStatusLabel(value string) string {
	switch value {
	case wartung.StatusOK:
		return "In Ordnung"
	case wartung.StatusSoon:
		return "Bald fällig"
	case wartung.StatusOverdue:
		return "Überfällig"
	default:
		return value
	}
}

func wartungUnitLabel(value string) string {
	switch value {
	case wartung.UnitDays:
		return "Tage"
	case wartung.UnitWeeks:
		return "Wochen"
	case wartung.UnitMonths:
		return "Monate"
	case wartung.UnitYears:
		return "Jahre"
	default:
		return value
	}
}

func wartungFindingLabel(value string) string {
	switch value {
	case wartung.FindingOK:
		return "In Ordnung"
	case wartung.FindingDefect:
		return "Mangel festgestellt"
	default:
		return "Kein Befund angegeben"
	}
}

func (a *app) makeWartungCard(object wartung.Object, settings wartung.Settings, now time.Time) (wartungCard, error) {
	due, err := wartung.NextDue(object.LastMaintenanceDate, object.IntervalValue, object.IntervalUnit)
	if err != nil {
		return wartungCard{}, err
	}
	state := wartung.Status(object, settings.WarningDays, now, a.store.TaskSettings().Location)
	warning := settings.WarningDays
	if object.WarningDays != nil {
		warning = *object.WarningDays
	}
	return wartungCard{Object: object, NextDueDate: due, State: state, StateLabel: wartungStatusLabel(state), EffectiveWarningDays: warning}, nil
}

func (a *app) wartungCards(objects []wartung.Object, settings wartung.Settings, now time.Time) ([]wartungCard, error) {
	cards := make([]wartungCard, 0, len(objects))
	for _, object := range objects {
		card, err := a.makeWartungCard(object, settings, now)
		if err != nil {
			return nil, err
		}
		cards = append(cards, card)
	}
	return cards, nil
}

func (a *app) wartungBoard(w http.ResponseWriter, r *http.Request, u core.User) {
	q := r.URL.Query()
	filter := wartung.Filter{Category: q.Get("category"), Status: q.Get("status"), Activity: q.Get("activity")}
	objects, err := a.store.WartungObjects(r.Context(), filter)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	settings, err := a.store.WartungSettings(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	categories, err := a.store.WartungCategories(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	cards, err := a.wartungCards(objects, settings, time.Now())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	a.render(w, r, "wartung", page{Title: "Werkstattwartung", Current: &u, WartungObjects: cards, WartungFilter: filter, WartungSettings: settings, Categories: categories})
}

func (a *app) newWartungObject(w http.ResponseWriter, r *http.Request, u core.User) {
	if !wartung.CanManageObjects(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	settings, err := a.store.WartungSettings(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	categories, err := a.store.WartungCategories(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	today := wartung.Today(time.Now(), a.store.TaskSettings().Location)
	object := wartung.Object{Category: "Allgemein", IntervalValue: 1, IntervalUnit: wartung.UnitMonths, LastMaintenanceDate: today, Active: true}
	a.render(w, r, "wartung-form", page{Title: "Neues Wartungsobjekt", Current: &u, Wartung: wartungCard{Object: object}, WartungSettings: settings, WartungToday: today, Categories: categories})
}

func wartungInput(r *http.Request) (wartung.Input, error) {
	value, err := strconv.Atoi(r.PostForm.Get("interval_value"))
	if err != nil {
		return wartung.Input{}, core.ErrValidation
	}
	var warning *int
	if raw := r.PostForm.Get("warning_days"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil {
			return wartung.Input{}, core.ErrValidation
		}
		warning = &n
	}
	return wartung.Input{
		Name: r.PostForm.Get("name"), Description: r.PostForm.Get("description"), Category: r.PostForm.Get("category"), Location: r.PostForm.Get("location"),
		IntervalValue: value, IntervalUnit: r.PostForm.Get("interval_unit"), LastMaintenanceDate: r.PostForm.Get("last_maintenance_date"), WarningDays: warning, Instructions: r.PostForm.Get("instructions"),
	}, nil
}

func (a *app) createWartungObject(w http.ResponseWriter, r *http.Request, u core.User) {
	if !wartung.CanManageObjects(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	in, err := wartungInput(r)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	object, err := a.store.CreateWartungObject(r.Context(), u.ID, in)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/wartung/%d", object.ID), http.StatusSeeOther)
}

func (a *app) editWartungObject(w http.ResponseWriter, r *http.Request, u core.User) {
	if !wartung.CanManageObjects(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	object, err := a.store.WartungObject(r.Context(), id)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	settings, err := a.store.WartungSettings(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	categories, err := a.store.WartungCategories(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	card, err := a.makeWartungCard(object, settings, time.Now())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	warningValue := ""
	if object.WarningDays != nil {
		warningValue = strconv.Itoa(*object.WarningDays)
	}
	a.render(w, r, "wartung-form", page{Title: "Wartungsobjekt bearbeiten", Current: &u, Editing: true, Wartung: card, WartungSettings: settings, WartungWarningValue: warningValue, Categories: categories})
}

func (a *app) updateWartungObject(w http.ResponseWriter, r *http.Request, u core.User) {
	if !wartung.CanManageObjects(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	version, err := formVersion(r)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	in, err := wartungInput(r)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	_, err = a.store.UpdateWartungObject(r.Context(), u.ID, id, wartung.Update{
		Name: in.Name, Description: in.Description, Category: in.Category, Location: in.Location, IntervalValue: in.IntervalValue, IntervalUnit: in.IntervalUnit,
		WarningDays: in.WarningDays, Instructions: in.Instructions, Active: r.PostForm.Get("active") == "1", Version: version,
	})
	if err != nil {
		a.failWartung(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/wartung/%d", id), http.StatusSeeOther)
}

func (a *app) wartungObject(w http.ResponseWriter, r *http.Request, u core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	object, err := a.store.WartungObject(r.Context(), id)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	settings, err := a.store.WartungSettings(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	history, err := a.store.WartungHistory(r.Context(), id)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	authors, err := a.store.ActiveAuthors(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	now := time.Now()
	card, err := a.makeWartungCard(object, settings, now)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	a.render(w, r, "wartung-object", page{Title: object.Name, Current: &u, Wartung: card, WartungRecords: history, WartungSettings: settings, WartungToday: wartung.Today(now, a.store.TaskSettings().Location), Authors: authors, CanApprove: wartung.CanApprove(u), CanManage: wartung.CanManageObjects(u)})
}

func (a *app) recordWartung(w http.ResponseWriter, r *http.Request, _ core.User) {
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	author, err := a.selectedAuthor(r)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	_, err = a.store.RecordWartung(r.Context(), author.ID, id, wartung.RecordInput{PerformedDate: r.PostForm.Get("performed_date"), Note: r.PostForm.Get("note"), Finding: r.PostForm.Get("finding")})
	if err != nil {
		a.failWartung(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/wartung/%d", id), http.StatusSeeOther)
}

func (a *app) approveWartung(w http.ResponseWriter, r *http.Request, u core.User) {
	if !wartung.CanApprove(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	id, err := entryID(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	recordID, err := positiveID(r.PathValue("recordID"), false)
	if err != nil {
		a.fail(w, core.ErrNotFound)
		return
	}
	_, err = a.store.ApproveWartung(r.Context(), u.ID, id, recordID)
	if err != nil {
		a.failWartung(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/wartung/%d#wartung-record-%d", id, recordID), http.StatusSeeOther)
}

func (a *app) wartungSettings(w http.ResponseWriter, r *http.Request, u core.User) {
	if !wartung.CanManageObjects(u) {
		a.fail(w, core.ErrForbidden)
		return
	}
	if r.Method == http.MethodPost {
		version, err := formVersion(r)
		if err != nil {
			a.failWartung(w, err)
			return
		}
		days, err := strconv.Atoi(r.PostForm.Get("warning_days"))
		if err != nil {
			a.failWartung(w, core.ErrValidation)
			return
		}
		if _, err = a.store.UpdateWartungSettings(r.Context(), u.ID, days, version); err != nil {
			a.failWartung(w, err)
			return
		}
		http.Redirect(w, r, "/admin/wartung/settings", http.StatusSeeOther)
		return
	}
	settings, err := a.store.WartungSettings(r.Context())
	if err != nil {
		a.failWartung(w, err)
		return
	}
	a.render(w, r, "wartung-settings", page{Title: "Wartung: Vorwarnzeit", Current: &u, WartungSettings: settings})
}

func (a *app) failWartung(w http.ResponseWriter, err error) {
	if errors.Is(err, core.ErrValidation) {
		http.Error(w, "Bitte gültige Wartungsangaben verwenden: Name und Kategorie sind Pflicht. Das Intervall muss zwischen 1 und 1000 liegen, die Vorwarnzeit zwischen 0 und 365 Tagen. Wartungsdaten dürfen nicht in der Zukunft liegen. Texte müssen innerhalb der angezeigten Grenzen bleiben.", http.StatusBadRequest)
		return
	}
	a.fail(w, err)
}
