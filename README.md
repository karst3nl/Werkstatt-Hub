# Werkstatt-Hub

Lokale Webapp für eine KFZ-Werkstatt. Meilenstein 1 stellt die gemeinsame Basis bereit: Nutzerauswahl, Rollen, signierte Basiseinträge mit Kommentaren und nachvollziehbare Änderungshistorie. Oberfläche und Fehlermeldungen sind deutsch.

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

```sh
CGO_ENABLED=0 go build -o bin/werkstatt-hub ./cmd/werkstatt-hub
./bin/werkstatt-hub serve --config config.local.json
# Raspberry Pi mit 64-Bit-Linux, auf dem Entwicklungsrechner gebaut:
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/werkstatt-hub-linux-arm64 ./cmd/werkstatt-hub
```

Für ein späteres Deployment werden nur das passende Binary, eine lokale Konfiguration und ein beschreibbares Datenverzeichnis benötigt. Auf dem Pi einen festen Arbeitsordner oder absolute Datenbankpfade verwenden. Das Programm beendet HTTP-Verbindungen bei SIGINT/SIGTERM geordnet. systemd-Service und automatisierte lokale/NAS-Backups folgen in Meilenstein 8. Bis dahin zum Sichern die App stoppen und das vollständige Datenverzeichnis kopieren.

## Rechte und Annahmen für Meilenstein 1

Die AGENTS.md beschreibt die Rollen, aber noch keine vollständige Rechtematrix. Deshalb gelten diese einfachen Annahmen:

| Aktion | Mitarbeiter (inkl. Azubi) | Vorgesetzter | Admin |
| --- | --- | --- | --- |
| Basiseinträge lesen, erstellen und kommentieren | Ja | Ja | Ja |
| Titel und Text ändern | Eigene Einträge | Alle | Alle |
| Status ändern | Nein | Ja | Ja |
| Audit-Log ansehen | Nein | Ja | Ja |
| Benutzer anlegen, umbenennen, deaktivieren, Rollen setzen | Nein | Nein | Ja |

- Die Anmeldung ist die ausdrücklich gewünschte **Namensauswahl ohne Passwort/PIN**, auch auf der separaten Admin-Anmeldeseite. Sie identifiziert die gewählte Person, prüft ihre Identität jedoch nicht. Wer das Werkstattnetz erreicht, kann auch einen Admin-Namen auswählen. Nur in diesem vertrauten lokalen Netz betreiben. Eine Signatur ist Name + Serverzeit, keine kryptografische Unterschrift.
- Rollenprüfungen erfolgen serverseitig. Inaktive Nutzer können sich nicht anmelden; bestehende Sitzungen berücksichtigen Deaktivierungen und Rollenänderungen sofort. Authentifizierung und Sitzungen sind im Webpaket gekapselt und können später um PIN/Passwort ergänzt werden. Sitzungen liegen nur im Arbeitsspeicher, laufen ab und enden bei Neustart.
- Jeder Basiseintrag beginnt mit Status `neu`. Weitere Basisstatus: `in_pruefung`, `geplant`, `erledigt`, `abgelehnt`. Modulspezifische Abläufe folgen mit den jeweiligen Modulen.
- Signaturen und Autor bleiben unverändert, auch nach einer Benutzerumbenennung. Kommentare sind in diesem Meilenstein nach dem Absenden unveränderlich. Zeiten werden in UTC gespeichert und angezeigt.
- Änderungen an Einträgen, Kommentare und Benutzerverwaltung werden atomar mit dem Audit-Ereignis gespeichert: Akteur als ID und damaliger Name, Serverzeit, Aktion und vollständiger vorheriger/neuer Zustand. Bei einem Audit-Fehler wird die gesamte Änderung zurückgerollt. Parallele Eintragsbearbeitungen erkennen veraltete Versionen und überschreiben diese nicht.
- Das Audit-Log ist über App und Datenbank-Trigger nur erweiterbar. Die gemeinsame Anwendungsschicht schützt alle Schreibvorgänge; direkter Zugriff auf die SQLite-Datei gehört zum vertrauenswürdigen Betrieb und ist kein unterstützter Schreibweg.
- Eine aktive Admin-Person muss erhalten bleiben. Der letzte aktive Admin kann weder deaktiviert noch heruntergestuft werden.
- Es gibt noch keinen Kummerkasten und keine anderen Fachmodule. Generische Basiseinträge sind für alle aktiven Nutzer sichtbar. Die beiden Prioritätsfelder (1–5, zunächst leer) sind im Modell vorbereitet; Setzen, Sortieren und Prio-Historie folgen in Meilenstein 2. Vertrauliche Kummerkasten-Inhalte erst im entsprechend geschützten späteren Modul erfassen.

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
internal/core/                 Gemeinsame Modelle und Rechte
internal/storage/              SQLite, Transaktionen und Audit-Log
internal/storage/migrations/   Versioniertes Schema
internal/web/                  HTTP, Sitzungen, Templates, lokale Assets
```

Tests verwenden ausschließlich temporäre Verzeichnisse. Datenbanken, Uploads, Sicherungen, Logs, lokale Konfiguration und Geheimnisse dürfen nicht versioniert werden; `.gitignore` schützt die üblichen Pfade und Dateiformate. Beispielkonfiguration enthält ausschließlich lokale Standardwerte. Änderungen ausschließlich auf Branch `alpha`, in kleinen thematischen Commits; niemals direkt auf `main` pushen.

Die Fachmodule bleiben getrennte spätere Pakete und nutzen die gemeinsame Benutzer-, Signatur-, Kommentar- und Audit-Basis. Ideenboard/Prioritäten, Azubi-Aufgaben, Werkzeuge, Materialien, Kummerkasten, erweiterte Administration/Uploads und Pi-Betrieb folgen gemäß `AGENTS.md`.
