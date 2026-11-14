# AGENTS.md – Werkstatt-Hub

Repo: https://github.com/karst3nl/Werkstatt-Hub

## 1. Ziel
Webapp für eine KFZ-Werkstatt. Sie bündelt:
1. **Ideenboard / Vorschläge** zum Arbeitsablauf
2. **Werkzeugverwaltung** (Zustand, Defekte, Neuanschaffungen)
3. **Kummerkasten**
4. **Azubi-Aufgabenboard** mit Erinnerungen
5. **Verbrauchsmaterialien** mit Bestandsprüfung und Startseiten-Warnung

Nutzer: Mitarbeiter, Azubis und Vorgesetzte. Sprache der Oberfläche: **Deutsch**.

## 2. Arbeitsregeln für Codex (Git)
- **Alle Arbeit im Branch `alpha`.** Nie direkt auf `main` pushen.
- Kleine, thematisch saubere Commits mit klaren Messages.
- Dateien pflegen: `README.md` (Setup, Start, Deployment), `CHANGELOG.md`, diese `AGENTS.md`.
- **Keine Daten im Repo:** Datenbank, Uploads, Backups, Logs und Konfigurationen mit Geheimnissen gehören in `.gitignore`. Beispielkonfiguration nur als `config.example.*`.
- Datenbankschema über versionierte Migrationen verwalten.
- Tests für Kernlogik (Rechte, Prio-System, Audit-Log, Bestandswarnung).

## 3. Technische Rahmenbedingungen
- Läuft später auf einem **Raspberry Pi** im **lokalen Werkstattnetz**. Kein Internetzugriff nötig, kein Zugriff von außen.
- **Daten verlassen das Pi nicht.** Keine externen Dienste, keine CDNs, keine Telemetrie. Alle Assets (Fonts, JS, CSS) lokal ausliefern.
- Datenbank: **SQLite**.
- Sprache und Framework: Codex wählt nach **Sinnhaftigkeit, guter Umsetzbarkeit und Wartbarkeit** und begründet die Wahl kurz in der README. Empfehlung: ein einzelnes, leicht deploybares Backend (z. B. Go mit SQLite) mit serverseitig gerenderten Seiten und wenig JavaScript. Ein schweres SPA-Framework ist nicht nötig.
- Ressourcenschonend (Pi), Start als systemd-Service.
- Oberfläche: mobil-, tablet- und desktoptauglich, große Bedienelemente (Werkstattumgebung).
- Größe: ca. **12 Nutzer** plus **1 Admin-Zugang**.

## 4. Rollen und Anmeldung
- Rollen: **Mitarbeiter** (inkl. Azubis), **Vorgesetzter**, **Admin**.
- **Anmeldung (erste Version):** einfache Auswahl des eigenen Namens aus einer Liste, kein Passwort. Das ist bewusst simpel, weil die App nur im Werkstattnetz läuft.
- Den Auth-Teil so kapseln, dass später PIN oder Passwort ergänzt werden kann, ohne den Rest umzubauen.
- Admin-Zugang separat (Benutzer anlegen, deaktivieren, Rollen setzen, Einstellungen).

## 5. Gemeinsame Basis aller Einträge
Jeder Eintrag (Idee, Werkzeugmeldung, Kummerkasten, Aufgabe, Verbrauchsmaterial-Meldung) hat:
- Autor, Erstellzeitpunkt, Status, Kommentare
- **Signatur:** Name und Zeitpunkt des Absenders, automatisch aus dem gewählten Nutzer, nicht manuell änderbar
- **Audit-Log:** jede Änderung (Status, Prio, Text) mit Wer und Wann, nichts wird still überschrieben
- **Zwei Prioritäten**, siehe Abschnitt 6

Keine Anonymität, auch nicht im Kummerkasten (erste Version).

## 6. Prioritätssystem
- `prio_mitarbeiter`: vom Ersteller bzw. Mitarbeitern auf einer Skala gesetzt.
- `prio_vorgesetzter`: unabhängig davon von Vorgesetzten nach deren Ansicht gesetzt, ebenfalls auf einer Skala.
- **Beide Prios sind für alle Nutzergruppen sichtbar.**
- Skala (Vorschlag, konfigurierbar): 1 bis 5. Codex darf eine andere sinnvolle Skala wählen, solange sie für beide Seiten gleich ist.
- Sortierung und Filter nach beiden Prios. Starke Abweichung zwischen beiden sichtbar machen (z. B. Icon).
- Jede Prio-Änderung wird im Audit-Log protokolliert.

## 7. Module

### 7.1 Ideenboard / Arbeitsablauf
Titel, Beschreibung, Kategorie, Status (neu, in Prüfung, geplant, umgesetzt, abgelehnt), Kommentare, beide Prios.

### 7.2 Werkzeugverwaltung
Der Fokus liegt auf **Zustand und Neuanschaffung**, nicht auf Ausleihe.
- Werkzeug: Name, Kategorie, Standort, Zustand (ok, verschleißt, defekt, ersetzt).
- Meldung, z. B. "Schraubenschlüssel ist kaputt", mit Signatur.
- Anschaffungswunsch (neues Werkzeug) mit Status (gewünscht, genehmigt, bestellt, geliefert) und beiden Prios.
- Historie pro Werkzeug.
- Später optional: Etikettendruck mit QR-Code.

### 7.3 Kummerkasten
Eintrag mit Titel, Text, Kategorie, Status, signiert. Sichtbarkeit und Rechte als konfigurierbare Einstellung vorbereiten (Standard: sichtbar für Vorgesetzte und Admin).

### 7.4 Azubi-Aufgabenboard
- Aufgaben pro Azubi abbilden (zugewiesen von Vorgesetzten oder Ausbildern).
- Kanban-Status: Offen, In Arbeit, Zur Kontrolle, Erledigt.
- Fälligkeitsdatum und **Erinnerungen**: in der App auf der Startseite und in der eigenen Aufgabenansicht (überfällig, heute fällig, bald fällig). Keine E-Mail oder Push-Benachrichtigungen in der ersten Version.
- Abnahme durch Vorgesetzte.

### 7.5 Verbrauchsmaterialien (Zusatzfunktion)
- Material: Name (z. B. Scheibenklar), Einheit (z. B. Kiste), aktueller Bestand, **Mindestbestand**, Lagerort.
- **Bestandsprüfung durch einen Azubi:** Der Azubi trägt den aktuellen Bestand ein (signiert, mit Zeitpunkt).
- Fällt der Bestand auf oder unter den Mindestbestand (Beispiel: nur noch eine Kiste Scheibenklar), erscheint auf der **Startseite eine Warnung** ("Scheibenklar sollte bestellt werden").
- Die Warnung muss von einem **Vorgesetzten abgehakt** werden (mit Signatur, im Audit-Log). Erst dann verschwindet sie. Sinkt der Bestand später erneut unter das Minimum, entsteht eine neue Warnung.
- Optional vorbereiten: Prüfintervalle (z. B. wöchentliche Bestandsprüfung als wiederkehrende Azubi-Aufgabe).

## 8. Startseite (Dashboard)
- Offene Warnungen (Verbrauchsmaterial) ganz oben, mit Abhaken-Funktion für Vorgesetzte.
- Meine Aufgaben und Erinnerungen.
- Neueste und höchstpriorisierte Einträge pro Bereich.

## 9. Admin-Bereich
- Benutzerverwaltung (anlegen, deaktivieren, Rolle).
- Stammdaten: Kategorien, Prioskala, Materialien, Mindestbestände.
- **Grundstein für Foto-Uploads:** Datenmodell, Speicherort und Upload-Mechanik vorbereiten, aber in der Oberfläche der ersten Version noch nicht ausspielen. Uploads liegen lokal auf dem Pi, außerhalb des Repos.

## 10. Datenschutz und Daten
- Alle Daten bleiben auf dem Pi.
- Nie Daten oder Backups ins Repo pushen.
- Regelmäßiges Backup der SQLite-Datei vorsehen (Ziel konfigurierbar, lokal oder NAS im Werkstattnetz).

## 11. Meilensteine (Vorschlag)
1. Projektgerüst, Nutzerauswahl, Rollen, Audit-Log, Basis-Eintragsmodell
2. Prio-System und Ideenboard
3. Azubi-Aufgabenboard mit Erinnerungen
4. Werkzeugverwaltung (Zustand, Anschaffung)
5. Verbrauchsmaterialien mit Startseiten-Warnung und Abhaken
6. Kummerkasten
7. Admin-Bereich inkl. Foto-Grundlagen
8. Deployment auf dem Pi (systemd), Backup, Feinschliff

## 12. Erweiterbarkeit
Jedes Modul ist ein eigenes Paket mit eigenem Datenmodell, nutzt aber gemeinsam Benutzer-, Prio-, Audit- und Kommentarlogik. Neue Bereiche sollen ohne Umbau des Kerns ergänzbar sein.

## 13. Bewusst noch nicht in Version 1
Passwörter oder PIN, Benachrichtigungen (Mail/Push), Foto-Anzeige in der Oberfläche, Anonymität, Zugriff von außen, Ausleihe-System für Werkzeuge.

## 14. Umsetzungsstand
- **Meilenstein 1 umgesetzt:** Go/SQLite-Projektgerüst, lokale Konfiguration, deutsche Namensauswahl mit separatem Admin-Zugang, Rollen, Basiseinträge und Kommentare mit unveränderlicher Signatur, atomarer Audit-Verlauf und versionierte Migrationen.
- Die grundlegende Benutzerverwaltung dient bereits dem Einrichten und Prüfen der Rollen. Stammdaten und Foto-Grundlagen bleiben Meilenstein 7.
- Rechtematrix, Betriebsannahmen, Startanleitung und Grenzen sind in `README.md` dokumentiert. Keine Beispieldaten werden mitgeliefert.
- Rechte-, Audit-, Migrations- und HTTP-Tests vorhanden. Go-Race-Tests, `go vet`, Browserablauf auf Desktop/Mobil und ARM64-Build geprüft; noch kein Lauf auf echter Pi-Hardware.
- **Meilenstein 2 umgesetzt:** eigenes Ideenmodell mit Kategorie und Status bis „umgesetzt“, gemeinsames Prioritätssystem mit konfigurierbarer Skala, Filtern/Sortieren und sichtbarer starker Abweichung. Prioritätsänderungen sind versioniert und atomar auditiert.
- Migration 002 erhält bestehende Meilenstein-1-Daten. Kategorien sind zunächst freie Pflichttexte; die genaue Rechtematrix und gemeinsame Werte ohne Abstimmung sind in `README.md` dokumentiert.
- Meilenstein-2-Rechte, Prioritätsänderungen, Filter/Sortierung, Audit-Rollback und Migration mit Bestandsdaten getestet. Race-Tests, `go vet`, Chromium-Ablauf bei 320–1360 Pixeln und ARM64-Build erfolgreich.
- **Nächster fachlicher Schritt: Meilenstein 3**, Azubi-Aufgabenboard mit Erinnerungen.
