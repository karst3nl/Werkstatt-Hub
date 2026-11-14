# AGENTS.md – Werkstatt-Hub

Repo: https://github.com/karst3nl/Werkstatt-Hub

## 1. Ziel
Webapp für eine KFZ-Werkstatt. Sie bündelt:
1. **Ideenboard / Vorschläge** zum Arbeitsablauf
2. **Werkzeugverwaltung** (Zustand, Defekte, Neuanschaffungen)
3. **Kummerkasten**
4. **Azubi-Aufgabenboard** mit Erinnerungen
5. **Verbrauchsmaterialien** mit Bestandsprüfung und Startseiten-Warnung
6. **Werkstattwartung** mit regelmäßigen Intervallen, Fälligkeiten und Durchführungshistorie gemäß `ERWEITERUNG_Werkstattwartung.md`

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
- **Startseite ohne Anmeldung:** Beim Aufrufen der App öffnet sich direkt das Dashboard. Ideenboard, Aufgabenboard, Werkzeugbestand, Zustandsmeldungen, Anschaffungswünsche, deren Detailansichten und vorhandene Kommentare sind im Werkstattnetz ohne Namenssitzung lesbar.
- **Erstellerwahl pro neuem Beitrag:** Beim Erstellen einer Idee, Werkzeugmeldung oder eines Anschaffungswunsches wird ein aktiver Ersteller aus der Namensliste ausdrücklich ausgewählt. Die Auswahl gilt nur für diesen Beitrag, erzeugt keine Sitzung und wechselt keine vorhandene Sitzung. Sie bestimmt Autor, Signatur und Audit-Akteur der Neuanlage.
- **Aufgabenzuweisung:** Neue Aufgaben benötigen eine aktive Vorgesetzten- oder Admin-Sitzung. Der ausdrücklich gewählte Ersteller muss ebenfalls ein aktiver Vorgesetzter oder Admin sein. Er signiert den Eintrag; Audit-Akteur bleibt der tatsächliche Sitzungsnutzer. Die Erstellerwahl wechselt keine Sitzung.
- **Werkzeugbestand:** Neue Werkzeuge benötigen wie Aufgaben eine aktive Vorgesetzten- oder Admin-Sitzung und einen ausdrücklich gewählten aktiven Vorgesetzten/Admin als Ersteller. Der gewählte Ersteller signiert; Audit-Akteur bleibt der handelnde Sitzungsnutzer. Bestand, offizieller Zustand und Beschaffungsstatus werden nur von Vorgesetzten/Admins gepflegt.
- **Werkstattwartung:** Objekte und Historien sind ohne Sitzung lesbar. Durchführungen werden mit ausdrücklich gewähltem aktivem Ersteller erfasst; dessen Signatur und Audit-Akteur werden serverseitig festgelegt, ohne Sitzungswechsel. Abzeichnen benötigt eine Vorgesetzten-/Admin-Sitzung; Objektverwaltung und globale Vorwarnzeit benötigen eine vollständig angemeldete Admin-Sitzung.
- **Admin-Zugang mit Passwort:** Beim ersten Serverstart wird das Konto „Admin“ automatisch angelegt und ein zufälliges Startpasswort einmalig in der lokalen Serverkonsole ausgegeben. Beim Upgrade erhalten bestehende aktive Admins ohne Passwort ebenfalls einmalig ein Startpasswort. Vor geschützten Aktionen muss ein neues Passwort festgelegt werden; bereits gesetzte Passwörter werden bei Neustarts nicht zurückgesetzt. Es gibt kein universelles Passwort im Repository.
- **Optionale Namenssitzung:** Mitarbeiter und Vorgesetzte wählen zum Kommentieren, Bewerten und Bearbeiten weiterhin den eigenen Namen aus einer Liste, ohne Passwort. Admins benötigen eine Passwortanmeldung; eine Namensauswahl oder Erstellerwahl gewährt keine Admin-Rechte. Geschützte Aktionen behalten ihre Rollenprüfungen.
- Den Auth-Teil so kapseln, dass später PIN oder Passwort ergänzt werden kann, ohne den Rest umzubauen.
- Nutzer dürfen ausschließlich von einem vollständig angemeldeten Admin erstellt, geändert, deaktiviert und gelöscht werden. Neue oder hochgestufte Admins erhalten ein temporäres Passwort mit Wechselpflicht. Der letzte aktive Admin kann nicht gelöscht, deaktiviert oder heruntergestuft werden. Gelöschte Nutzer verschwinden aus aktuellen Listen; historische IDs, Signaturen und Audit-Verweise bleiben erhalten.

## 5. Gemeinsame Basis aller Einträge
Jeder Eintrag (Idee, Werkzeugmeldung, Kummerkasten, Aufgabe, Verbrauchsmaterial-Meldung) hat:
- Autor, Erstellzeitpunkt, Status, Kommentare
- **Signatur:** Name und Zeitpunkt des Absenders, automatisch aus dem gewählten Nutzer, nicht manuell änderbar
- **Audit-Log:** jede Änderung (Status, Prio, Text) mit Wer und Wann, nichts wird still überschrieben
- **Zwei Prioritäten**, siehe Abschnitt 6

Keine Anonymität, auch nicht im Kummerkasten (erste Version).

Werkstattwartung verwendet gemäß Erweiterungsbeschreibung eigene Wartungsobjekte und unveränderliche Durchführungseinträge mit Signatur und Audit. Zusätzliche Kommentare und die beiden Prioritäten werden dafür in dieser Stufe nicht eingeführt; maßgeblich sind Intervall, Kalenderfälligkeit und Befund.

## 6. Prioritätssystem
- `prio_mitarbeiter`: vom Ersteller bzw. Mitarbeitern auf einer Skala gesetzt.
- `prio_vorgesetzter`: unabhängig davon von Vorgesetzten nach deren Ansicht gesetzt, ebenfalls auf einer Skala.
- **Beide Prios sind für alle Nutzergruppen sichtbar.**
- Skala (Vorschlag, konfigurierbar): 1 bis 5. Codex darf eine andere sinnvolle Skala wählen, solange sie für beide Seiten gleich ist.
- Sortierung und Filter nach beiden Prios. Starke Abweichung zwischen beiden sichtbar machen (z. B. Icon).
- Jede Prio-Änderung wird im Audit-Log protokolliert.
- Darstellung mit fünf lokalen SVG-Schraubenschlüsseln: gesetzte Stufen werden von Grün über Gelb/Orange bis Rot gefüllt, übrige Symbole bleiben grau umrandet. Zugängliche Beschriftungen nennen Wert und Skala. Andere konfigurierte Skalen werden nur für die Anzeige auf fünf Stufen abgebildet; gespeicherte Werte bleiben unverändert.

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

### 7.6 Werkstattwartung (Zusatzmeilenstein)
- Spezifikation: `ERWEITERUNG_Werkstattwartung.md`. Eigenes Fachpaket `internal/wartung`.
- Admins pflegen Wartungsobjekte mit Name, Kategorie, Standort, Beschreibung/Hinweisen, Intervall und Vorwarnzeit; Objekte werden deaktiviert statt gelöscht.
- Intervalle in Tagen, Wochen, Monaten oder Jahren. Die nächste Fälligkeit folgt dem neuesten Datum aus Startwert und Durchführungen plus aktuellem Intervall. Monats-/Jahreswechsel begrenzen ungültige Tage auf das Monatsende; Tagesgrenzen folgen der Werkstatt-Zeitzone.
- Globale Vorwarnzeit zunächst sieben Kalendertage, vom Admin in der Datenbank einstellbar. Leere Objektvorwarnzeit übernimmt den aktuellen globalen Wert; eigener Wert 0–365 Tage. Heute gilt als bald fällig, ab dem Folgetag als überfällig.
- Durchführung mit ausdrücklich gewähltem aktivem Nutzer, Datum bis einschließlich heute, optionaler Notiz und Befund. Rückwirkende Nachträge bleiben erhalten und drehen neuere Termine nicht zurück. Der Startwert bleibt nach der Anlage unveränderlich.
- Vorgesetzte/Admins können Durchführungseinträge einmalig signiert abzeichnen. Die Fälligkeit wird bereits beim Erfassen der Durchführung neu berechnet. Historie und Abzeichnungen sind unveränderlich, auch bei Nutzerumbenennung/-löschung und Objektdeaktivierung.
- Öffentliche Übersicht/Details mit Filtern und deutschen Datumswerten; Wartungsblock und hervorgehobene Überfälligkeiten oben im Dashboard. Deaktivierte Objekte erzeugen keine Warnungen und erhalten keine neuen Durchführungseinträge.
- Keine automatische Aufgabenanlage, Foto-Anzeige, Benachrichtigungen oder externen Dienste in dieser Stufe.

## 8. Startseite (Dashboard)
- Desktop-Anordnung: Aufgaben links, Werkzeuge und Anschaffungen mittig, Ideen rechts. Auf schmalen Bildschirmen in derselben Reihenfolge untereinander. Generische Basiseinträge entfallen aus Oberfläche und `/entries/*`; vorhandene historische Daten werden nicht gelöscht.
- Offene Warnungen (Verbrauchsmaterial) ganz oben, mit Abhaken-Funktion für Vorgesetzte.
- Wartungsblock mit Anzahl bald fälliger/überfälliger aktiver Objekte und Links zur Übersicht; überfällige Wartungen oberhalb der bisherigen Modulspalten hervorheben.
- Meine Aufgaben und Erinnerungen.
- Neueste und höchstpriorisierte Einträge pro Bereich.

## 9. Admin-Bereich
- Benutzerverwaltung ausschließlich durch vollständig angemeldete Admins (anlegen, ändern, deaktivieren, löschen, Rollen setzen).
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

Zusatzmeilenstein **Werkstattwartung** gemäß `ERWEITERUNG_Werkstattwartung.md` auf ausdrücklichen Nutzerwunsch nach Meilenstein 4 vorgezogen. Verbrauchsmaterialien bleiben ein eigenständiger, noch offener Meilenstein 5.

## 12. Erweiterbarkeit
Jedes Modul ist ein eigenes Paket mit eigenem Datenmodell, nutzt aber gemeinsam Benutzer-, Prio-, Audit- und Kommentarlogik. Neue Bereiche sollen ohne Umbau des Kerns ergänzbar sein.

## 13. Bewusst noch nicht in Version 1
Passwörter oder PIN für Mitarbeiter und Vorgesetzte (Admin-Passwörter sind umgesetzt), Benachrichtigungen (Mail/Push), Foto-Anzeige in der Oberfläche, Anonymität, Zugriff von außen, Ausleihe-System für Werkzeuge.

## 14. Umsetzungsstand
- **Meilenstein 1 umgesetzt:** Go/SQLite-Projektgerüst, lokale Konfiguration, deutsche Namensauswahl mit separatem Admin-Zugang, Rollen, Basiseinträge und Kommentare mit unveränderlicher Signatur, atomarer Audit-Verlauf und versionierte Migrationen.
- Die grundlegende Benutzerverwaltung dient bereits dem Einrichten und Prüfen der Rollen. Stammdaten und Foto-Grundlagen bleiben Meilenstein 7.
- Rechtematrix, Betriebsannahmen, Startanleitung und Grenzen sind in `README.md` dokumentiert. Keine Beispieldaten werden mitgeliefert.
- Rechte-, Audit-, Migrations- und HTTP-Tests vorhanden. Go-Race-Tests, `go vet`, Browserablauf auf Desktop/Mobil und ARM64-Build geprüft; noch kein Lauf auf echter Pi-Hardware.
- **Meilenstein 2 umgesetzt:** eigenes Ideenmodell mit Kategorie und Status bis „umgesetzt“, gemeinsames Prioritätssystem mit konfigurierbarer Skala, Filtern/Sortieren und sichtbarer starker Abweichung. Prioritätsänderungen sind versioniert und atomar auditiert.
- Migration 002 erhält bestehende Meilenstein-1-Daten. Kategorien sind zunächst freie Pflichttexte; die genaue Rechtematrix und gemeinsame Werte ohne Abstimmung sind in `README.md` dokumentiert.
- Meilenstein-2-Rechte, Prioritätsänderungen, Filter/Sortierung, Audit-Rollback und Migration mit Bestandsdaten getestet. Race-Tests, `go vet`, Chromium-Ablauf bei 320–1360 Pixeln und ARM64-Build erfolgreich.
- Ablaufkorrektur nach Nutzerwunsch: Dashboard und öffentliche Eintragsansichten direkt erreichbar; Erstellerpflichtwahl für jede Neuanlage, ohne impliziten Sitzungswechsel. Signaturen und Audit bleiben serverseitig, geschützte Aktionen weiterhin rollenabhängig.
- **Meilenstein 3 umgesetzt:** eigenes Aufgabenmodul mit öffentlichem Kanban-Board, persönlicher Aufgabenansicht, Fälligkeiten, Erinnerungen und signierter Abnahme durch Vorgesetzte/Admins.
- Aufgaben werden mangels eigener Azubi-Kennzeichnung zunächst aktiven Mitarbeitern zugewiesen. Zugewiesene Mitarbeiter bearbeiten die ersten drei Status; Zuweisung, Inhaltsänderungen, Abnahme und Wiederöffnen sind Vorgesetzten/Admins vorbehalten. Erledigte Aufgaben werden vor Inhaltsänderungen wieder geöffnet; eine Neuzuweisung setzt den Status auf „Offen“.
- Erinnerungen verwenden die Werkstatt-Zeitzone (Standard `Europe/Berlin`) und standardmäßig die nächsten drei Kalendertage. Zeitzone und Zeitraum sind konfigurierbar; Signaturzeiten bleiben UTC. Keine E-Mail-/Push-Dienste.
- Migration 003 ergänzt Aufgaben unter Erhalt der bisherigen Einträge, Ideen, Kommentare, Prioritäten, Signaturen und Audit-Historie. Prüfschwerpunkte für Rechte, Status/Abnahme, Kalendergrenzen und Migration sind in `README.md` festgehalten.
- Meilenstein 3 geprüft mit vollständigen Race-Tests, `go vet`, Builds ohne CGO für AMD64/ARM64 sowie einem Chromium-Ablauf bei 320–1360 Pixeln. Migration einer bestehenden Meilenstein-2-Testdatenbank erhält die bisherigen Daten.
- **Meilenstein 4 umgesetzt:** Werkzeugbestand mit Zustand, Kategorie und Standort; signierte Zustandsmeldungen; Anschaffungswünsche mit beiden Prioritäten und Beschaffungsstatus; zugehörige Historie pro Werkzeug.
- Werkzeugmeldungen ändern den offiziellen Zustand nicht automatisch. Offene Meldungen werden markiert; Vorgesetzte/Admins pflegen den Bestand und schließen die Meldung ausdrücklich. Werkzeugbezüge bleiben nach der Anlage unveränderlich. Gelieferte Wünsche werden nicht automatisch zu Inventareinträgen.
- Meldungen und Wünsche sind mit aktiver Erstellerwahl ohne Sitzung anlegbar. Mitarbeiter bearbeiten eigene Wünsche nur im Status „Gewünscht“; Beschaffungsstatus und Bestandsdaten sind Vorgesetzten/Admins vorbehalten. Die öffentliche Werkzeughistorie umfasst ausschließlich konkret zugehörige Ereignisse; das vollständige Audit bleibt geschützt.
- Migration 004 ergänzt das Werkzeugmodul unter Erhalt der bisherigen IDs, Aufgabenabnahmen, Kommentare, Prioritäten, Signaturen und Audit-Historie. Prüfschwerpunkte für Rechte, Zustand/Meldung, Historienabgrenzung und Migration sind in `README.md` festgehalten.
- Meilenstein 4 geprüft mit vollständigen Race-Tests, `go vet`, Builds ohne CGO für AMD64/ARM64 und einem Chromium-Ablauf bei 320–1360 Pixeln. Migration einer bestehenden Meilenstein-3-Testdatenbank erhält die bisherigen Daten einschließlich Aufgaben und Abnahmen.
- Ablaufkorrektur nach Nutzerwunsch: Generische Basiseinträge sind aus Oberfläche und HTTP-Routen entfernt; bestehende Basisdaten und Audit-Historie bleiben erhalten.
- Admin-Zugang mit automatisch erzeugtem einmaligem Startpasswort und verpflichtendem Passwortwechsel. Nur gesalzene Passwort-Hashes werden gespeichert; Passwörter/Hashes erscheinen nicht im Audit oder in Benutzeransichten. Passwort- und Rollenänderungen widerrufen bisherige Sitzungen.
- Nutzerverwaltung einschließlich Löschung bleibt vollständig Admins vorbehalten. Gelöschte Nutzer werden inaktiv und aus aktuellen Listen entfernt; historische Datensätze, Signaturen und Fremdschlüssel bleiben bestehen. Ein wiederverwendeter Name erhält eine neue Nutzer-ID.
- Prioritäten werden mit fünf farbigen lokalen Schraubenschlüsseln dargestellt, von Grün bis Rot; unbewertete Stufen bleiben grau. Die Anzeige verändert keine gespeicherten Prioritätswerte.
- Version 0.4.2 geprüft mit vollständigen Race-Tests, `go vet`, Builds ohne CGO für AMD64/ARM64 und einem Chromium-Ablauf bei 320–1440 Pixeln. Migration 005 erhält historische Daten; Admin-Ersteinrichtung, Pflichtwechsel, Sitzungswiderruf, Nutzerlöschung, Prioritätsanzeige und Passwortbestand bei Neustart sind geprüft.
- **Zusatzmeilenstein Werkstattwartung umgesetzt:** eigenes Datenmodell, zentral berechnete Kalenderfälligkeit, administrative Objekt-/Intervall-/Vorwarnverwaltung, öffentliche Übersicht und unveränderliche Durchführungshistorie mit Abzeichnung sowie Dashboard-Wartungsblock.
- Migration 006 ergänzt die Wartungstabellen und globalen Standardwerte ohne Änderung bestehender Tabellen/Daten. Alle Wartungsschreibvorgänge werden atomar auditiert; Signaturen, Durchführungshistorie und Abzeichnungen bleiben erhalten. Die Annahmen und Prüfschwerpunkte sind in `README.md` dokumentiert.
- Version 0.5.0 geprüft mit vollständigen Race-Tests, `go vet`, Builds ohne CGO für AMD64/ARM64 und einem Chromium-Ablauf bei 320–1440 Pixeln. Kalendergrenzen, Intervalländerung, rückwirkende Erfassung, Rechte, parallele Abzeichnung, Audit-Rollback aller Wartungsschreibwege, Migration 006 mit Bestandsdaten und mobile Darstellung sind geprüft.
- **Nächster fachlicher Schritt: Meilenstein 5**, Verbrauchsmaterialien mit Bestandswarnungen und Abhaken durch Vorgesetzte.
