# Werkstatt-Hub

Lokale Webapp für eine KFZ-Werkstatt. Meilensteine 1 und 2 sind umgesetzt: Nutzerauswahl, Rollen, signierte Einträge mit Kommentaren und Änderungshistorie sowie ein Ideenboard mit zwei unabhängigen Prioritäten. Oberfläche und Fehlermeldungen sind deutsch.

## Tech-Stack

**Go + SQLite + HTML-Templates**, ohne SPA-Framework. Go liefert einen ressourcenschonenden HTTP-Server und ein einzelnes ausführbares Programm; SQLite benötigt keinen eigenen Datenbankdienst und reicht für etwa zwölf Nutzer bequem aus. Der reine Go-Treiber `modernc.org/sqlite` ermöglicht Builds für Raspberry Pi ohne C-Toolchain. Templates, CSS und Migrationen sind eingebettet. Das hält Installation und Wartung einfach. Im Betrieb gibt es keine externen Dienste, CDNs, Fonts, JavaScript-Abhängigkeiten oder Telemetrie.

## Lokal starten

Voraussetzung: **Go 1.26 oder neuer**. Für das erstmalige Laden der versionierten Go-Abhängigkeiten braucht der Entwicklungsrechner Internet. Die fertige App läuft offline.

```sh
git clone --branch alpha https://github.com/karst3nl/Werkstatt-Hub.git
cd Werkstatt-Hub
go mod download
go run ./cmd/werkstatt-hub init --admin "Werkstatt Admin"
go run ./cmd/werkstatt-hub serve
```

Öffnen: **http://127.0.0.1:8080**. Beim ersten Start über **Admin-Anmeldung** (`/admin/login`) den eingerichteten Admin wählen. Unter **Benutzerverwaltung** Mitarbeiter und Vorgesetzte anlegen. Diese erscheinen in der normalen Namensliste. Es werden keine Beispielnutzer oder Einträge automatisch angelegt. `init` funktioniert ausschließlich bei einer noch leeren Benutzerliste; erneuter Aufruf überschreibt nichts.

Standardmäßig liegen sämtliche Laufzeitdaten **außerhalb des Checkouts** unter `../werkstatt-hub-data/werkstatt-hub.db`. Alle relativen Pfade beziehen sich auf das aktuelle Arbeitsverzeichnis, nicht auf den Ort der Konfigurationsdatei. SQLite erzeugt bei laufendem Betrieb gegebenenfalls zusätzliche WAL-/SHM-Dateien im selben Verzeichnis.

## Konfiguration und Build

Ohne Konfigurationsdatei gelten die Werte aus `config.example.json`. Für eigene Werte:

```sh
cp config.example.json config.local.json
go run ./cmd/werkstatt-hub init --config config.local.json --admin "Werkstatt Admin"
go run ./cmd/werkstatt-hub serve --config config.local.json
```

| Feld | Bedeutung |
| --- | --- |
| `listen` | Standard `127.0.0.1:8080`; für das Werkstattnetz z. B. `0.0.0.0:8080` |
| `database` | Pfad zur lokalen SQLite-Datei |
| `session_ttl` | Sitzungsdauer als Go-Dauer, Standard `12h`, maximal `168h` |
| `secure_cookies` | Für lokales HTTP `false`; bei späterem HTTPS `true` |
| `priority_min`, `priority_max` | Gemeinsame Skala für beide Prioritäten; Standard 1–5, zulässig `1 ≤ Minimum < Maximum ≤ 100` |

```sh
CGO_ENABLED=0 go build -o bin/werkstatt-hub ./cmd/werkstatt-hub
./bin/werkstatt-hub serve --config config.local.json
# Raspberry Pi mit 64-Bit-Linux, auf dem Entwicklungsrechner gebaut:
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/werkstatt-hub-linux-arm64 ./cmd/werkstatt-hub
```

Für ein späteres Deployment werden nur das passende Binary, eine lokale Konfiguration und ein beschreibbares Datenverzeichnis benötigt. Auf dem Pi einen festen Arbeitsordner oder absolute Datenbankpfade verwenden. Das Programm beendet HTTP-Verbindungen bei SIGINT/SIGTERM geordnet. systemd-Service und automatisierte lokale/NAS-Backups folgen in Meilenstein 8. Bis dahin zum Sichern die App stoppen und das vollständige Datenverzeichnis kopieren.

## Ideenboard und Prioritäten

Über **Ideenboard** (`/ideas`) neue Ideen mit Titel, Beschreibung und Kategorie vorschlagen. Kategorien sind vorerst frei benannte Pflichtfelder mit höchstens 100 Zeichen; bereits verwendete Kategorien werden vorgeschlagen. Der Startwert im Formular ist „Allgemein“. Eine zentrale Kategorienverwaltung folgt in Meilenstein 7.

Eine Idee beginnt als **Neu**. Vorgesetzte und Admins können den Status auf **In Prüfung**, **Geplant**, **Umgesetzt** oder **Abgelehnt** setzen. Autor und ursprüngliche Signatur bleiben erhalten. Titel, Beschreibung, Kategorie, Status und Prioritäten verwenden dieselbe Versionsprüfung, damit veraltete Formulare keine inzwischen erfolgte Änderung überschreiben.

Beide Prioritäten sind für alle aktiven Nutzer sichtbar und haben dieselbe konfigurierbare Skala. Größere Zahlen bedeuten höhere Priorität. Die Werte können auch bei den bisherigen Basiseinträgen gesetzt werden:

- **Mitarbeiter-Priorität:** Mitarbeiter dürfen sie bei allen Einträgen setzen. Der jeweilige Ersteller darf sie unabhängig von seiner Rolle bei eigenen Einträgen setzen. Vorgesetzte und Admins dürfen den Mitarbeiterwert anderer Ersteller nicht überschreiben.
- **Vorgesetzten-Priorität:** Vorgesetzte und Admins dürfen sie bei allen Einträgen setzen.
- Das sind zwei **gemeinsam gepflegte Werte**, keine Abstimmung pro Nutzer. Ein berechtigter Nutzer kann den jeweiligen Wert ersetzen oder mit „Noch nicht gesetzt“ löschen. Eine tatsächliche Änderung erhöht die Version und speichert Akteur, Zeitpunkt und vorherigen/neuen Stand atomar im Audit-Log. Unverändertes Speichern erzeugt keinen zusätzlichen Audit-Eintrag.
- Eine **starke Abweichung** wird als Symbol und Text markiert, sobald beide Werte gesetzt sind und ihre Differenz mindestens `(Maximum − Minimum) / 2 + 1` beträgt (ganzzahlig abgerundet vor `+ 1`). Bei der Standardskala 1–5 sind das mindestens drei Stufen.

Das Board filtert nach Kategorie, Status, beiden Prioritäten (auch „Noch nicht gesetzt“) und starken Abweichungen. Alle gesetzten Filter werden kombiniert. Sortierung nach Mitarbeiter- oder Vorgesetzten-Priorität ist absteigend; unbewertete Ideen stehen am Ende. Bei Gleichstand erscheinen neuere Ideen zuerst.

Die Skala wird über `priority_min` und `priority_max` in der lokalen JSON-Konfiguration geändert. Die App dazu neu starten. Fehlen diese Angaben in einer bisherigen Konfiguration, gilt weiterhin 1–5. Eine Skala, die bereits gespeicherte Werte ausschließt, führt zu einem verständlichen Startfehler; bestehende Werte werden nicht umgerechnet oder gelöscht.

## Rechte und weitere Annahmen

Die AGENTS.md beschreibt die Rollen, aber noch keine vollständige Rechtematrix. Deshalb gelten diese einfachen Annahmen:

| Aktion | Mitarbeiter (inkl. Azubi) | Vorgesetzter | Admin |
| --- | --- | --- | --- |
| Basiseinträge lesen, erstellen und kommentieren | Ja | Ja | Ja |
| Titel, Text und Ideenkategorie ändern | Eigene Einträge | Alle | Alle |
| Status ändern | Nein | Ja | Ja |
| Mitarbeiter-Priorität setzen/löschen | Alle Einträge | Eigene Einträge | Eigene Einträge |
| Vorgesetzten-Priorität setzen/löschen | Nein | Alle Einträge | Alle Einträge |
| Audit-Log ansehen | Nein | Ja | Ja |
| Benutzer anlegen, umbenennen, deaktivieren, Rollen setzen | Nein | Nein | Ja |

- Die Anmeldung ist die ausdrücklich gewünschte **Namensauswahl ohne Passwort/PIN**, auch auf der separaten Admin-Anmeldeseite. Sie identifiziert die gewählte Person, prüft ihre Identität jedoch nicht. Wer das Werkstattnetz erreicht, kann auch einen Admin-Namen auswählen. Nur in diesem vertrauten lokalen Netz betreiben. Eine Signatur ist Name + Serverzeit, keine kryptografische Unterschrift.
- Rollenprüfungen erfolgen serverseitig. Inaktive Nutzer können sich nicht anmelden; bestehende Sitzungen berücksichtigen Deaktivierungen und Rollenänderungen sofort. Authentifizierung und Sitzungen sind im Webpaket gekapselt und können später um PIN/Passwort ergänzt werden. Sitzungen liegen nur im Arbeitsspeicher, laufen ab und enden bei Neustart.
- Jeder Basiseintrag beginnt mit Status `neu`. Weitere Basisstatus: `in_pruefung`, `geplant`, `erledigt`, `abgelehnt`. Modulspezifische Abläufe folgen mit den jeweiligen Modulen.
- Signaturen und Autor bleiben unverändert, auch nach einer Benutzerumbenennung. Kommentare sind in diesem Meilenstein nach dem Absenden unveränderlich. Zeiten werden in UTC gespeichert und angezeigt.
- Änderungen an Einträgen, Kommentare und Benutzerverwaltung werden atomar mit dem Audit-Ereignis gespeichert: Akteur als ID und damaliger Name, Serverzeit, Aktion und vollständiger vorheriger/neuer Zustand. Bei einem Audit-Fehler wird die gesamte Änderung zurückgerollt. Parallele Eintragsbearbeitungen erkennen veraltete Versionen und überschreiben diese nicht.
- Das Audit-Log ist über App und Datenbank-Trigger nur erweiterbar. Die gemeinsame Anwendungsschicht schützt alle Schreibvorgänge; direkter Zugriff auf die SQLite-Datei gehört zum vertrauenswürdigen Betrieb und ist kein unterstützter Schreibweg.
- Eine aktive Admin-Person muss erhalten bleiben. Der letzte aktive Admin kann weder deaktiviert noch heruntergestuft werden.
- Ideen und generische Basiseinträge sind für alle aktiven Nutzer sichtbar. Es gibt noch keinen Kummerkasten und keine weiteren Fachmodule. Vertrauliche Kummerkasten-Inhalte erst im entsprechend geschützten späteren Modul erfassen.

## Bestehende Installation aktualisieren

Bei einem Update von Meilenstein 1 zuerst die App stoppen und das vollständige Datenverzeichnis sichern. Dann Branch `alpha` aktualisieren, das neue Programm bauen bzw. mit `go run` starten und dieselbe Konfiguration sowie Datenbank verwenden. **`init` nicht erneut ausführen.** Die neue versionierte Migration `002_ideas_priorities.sql` wird beim Start automatisch angewendet. IDs, Nutzer, Signaturen, Kommentare und Audit-Historie der bisherigen Einträge bleiben erhalten. Migration 001 wird nicht verändert. Eine bereits aktualisierte Datenbank benötigt mindestens diese Programmversion; für eine Rückkehr zum alten Programm die vorherige Sicherung verwenden.

## Datenbank und Entwicklung

```sh
go test ./...
go test -race ./...
go vet ./...
# Migrationen separat anwenden (auch bei jedem App-Start automatisch):
go run ./cmd/werkstatt-hub migrate --config config.local.json
```

Versionierte SQL-Migrationen liegen in `internal/storage/migrations/` und werden mit dem Programm ausgeliefert. Jede Version wird transaktional genau einmal angewendet und mit Prüfsumme verzeichnet. Bereits angewendete Migrationen nicht ändern; neue Schemaänderungen als nächste aufsteigende Nummer hinzufügen. Fehlende, geänderte oder unbekannte Migrationen führen zu einem Startfehler. Vor Schemaänderungen eine Sicherung erstellen; automatische Down-Migrationen gibt es nicht.

```text
cmd/werkstatt-hub/              Start, einmalige Admin-Einrichtung, Migrationen
internal/config/               Lokale JSON-Konfiguration
internal/core/                 Gemeinsame Modelle, Rechte und Prioritätsskala
internal/ideas/                Eigenes Ideenmodell und Statusregeln
internal/storage/              SQLite, Transaktionen und Audit-Log
internal/storage/migrations/   Versioniertes Schema
internal/web/                  HTTP, Sitzungen, Templates, lokale Assets
```

Tests verwenden ausschließlich temporäre Verzeichnisse. Datenbanken, Uploads, Sicherungen, Logs, lokale Konfiguration und Geheimnisse dürfen nicht versioniert werden; `.gitignore` schützt die üblichen Pfade und Dateiformate. Beispielkonfiguration enthält ausschließlich lokale Standardwerte. Änderungen ausschließlich auf Branch `alpha`, in kleinen thematischen Commits; niemals direkt auf `main` pushen.

Das Ideenboard ist das erste eigene Fachpaket und nutzt die gemeinsame Benutzer-, Signatur-, Kommentar-, Prioritäts- und Audit-Basis. Als nächster Schritt folgen die Azubi-Aufgaben mit Erinnerungen (Meilenstein 3), danach die weiteren Fachmodule, erweiterte Administration/Uploads und Pi-Betrieb gemäß `AGENTS.md`.

Die Tests decken Rechte, Audit-Rollback, unveränderliche Signaturen, Migration bestehender Meilenstein-1-Daten, konfigurierbare Prioritäten, Filter/Sortierung und Konflikte bei parallelen Bearbeitungen ab. Für Version 0.2.0 sind `go test -race ./...`, `go vet ./...` sowie Builds ohne CGO für Linux/AMD64 und Linux/ARM64 erfolgreich. Der vollständige Chromium-Ablauf prüft Ideen, Kommentare, Status, beide Prioritäten, Filter, Sortierung und Audit bei 320, 390, 768 und 1360 Pixeln; alle geladenen Ressourcen bleiben lokal. Zusätzlich wurde eine bestehende Meilenstein-1-Testdatenbank aktualisiert und der unveränderte Inhalt von Nutzern, Einträgen, Kommentaren und Audit-Log verglichen. Getestete Toolchain: Go 1.27.4. Der ARM64-Build wurde noch nicht auf echter Pi-Hardware ausgeführt.
