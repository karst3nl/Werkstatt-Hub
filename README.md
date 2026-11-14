# Werkstatt-Hub

Lokale Webapp für eine KFZ-Werkstatt. Meilensteine 1 bis 4 sind umgesetzt: direkt zugängliches Dashboard, Erstellerwahl pro neuem Beitrag, Rollen, signierte Einträge mit Kommentaren und Änderungshistorie, Ideenboard mit zwei unabhängigen Prioritäten, ein Aufgabenboard mit Fälligkeiten und Abnahme sowie Werkzeugbestand, Zustandsmeldungen und Anschaffungswünsche. Oberfläche und Fehlermeldungen sind deutsch.

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

Öffnen: **http://127.0.0.1:8080**. Das Dashboard erscheint direkt ohne Anmeldung. Zum Einrichten über **Admin** (`/admin/login`) den eingerichteten Admin wählen. Unter **Benutzerverwaltung** Mitarbeiter und Vorgesetzte anlegen. Diese erscheinen bei der Erstellerwahl in neuen Beiträgen und in der optionalen Namensauswahl für Bearbeitungen. Es werden keine Beispielnutzer oder Einträge automatisch angelegt. `init` funktioniert ausschließlich bei einer noch leeren Benutzerliste; erneuter Aufruf überschreibt nichts.

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
| `timezone` | Werkstatt-Zeitzone für Fälligkeiten, Standard `Europe/Berlin` |
| `reminder_days` | Zeitraum für „bald fällig“ in Kalendertagen, Standard `3`, zulässig 1–30 |

```sh
CGO_ENABLED=0 go build -o bin/werkstatt-hub ./cmd/werkstatt-hub
./bin/werkstatt-hub serve --config config.local.json
# Raspberry Pi mit 64-Bit-Linux, auf dem Entwicklungsrechner gebaut:
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/werkstatt-hub-linux-arm64 ./cmd/werkstatt-hub
```

Für ein späteres Deployment werden nur das passende Binary, eine lokale Konfiguration und ein beschreibbares Datenverzeichnis benötigt. Auf dem Pi einen festen Arbeitsordner oder absolute Datenbankpfade verwenden. Das Programm beendet HTTP-Verbindungen bei SIGINT/SIGTERM geordnet. systemd-Service und automatisierte lokale/NAS-Backups folgen in Meilenstein 8. Bis dahin zum Sichern die App stoppen und das vollständige Datenverzeichnis kopieren.

## Dashboard und Erstellerwahl

Die Startseite öffnet unmittelbar das **Werkstatt-Dashboard**, ohne Anmeldefenster. Basiseinträge, Ideenboard, Aufgabenboard, Werkzeugbestand, Zustandsmeldungen, Anschaffungswünsche, deren Detailansichten und vorhandene Kommentare können im Werkstattnetz ohne Namenssitzung gelesen werden. Auch nach Abmeldung, abgelaufener Sitzung oder Deaktivierung eines zuvor gewählten Namens bleibt das Dashboard erreichbar.

Bei **jedem neuen Basiseintrag, jeder Idee, jeder Werkzeugmeldung und jedem Anschaffungswunsch** ist im Formular ein aktiver **Ersteller** aus der Namensliste auszuwählen. Die Auswahl beginnt leer, auch bei einer bestehenden Sitzung. Name, Autor-ID und Signaturzeit werden serverseitig aus diesem Nutzer und der aktuellen Uhrzeit erzeugt. Freie Signatur- oder Zeitangaben aus dem Browser werden nicht übernommen. Fehlende, unbekannte und deaktivierte Ersteller werden abgewiesen.

Die Erstellerwahl gilt ausschließlich für den neuen Beitrag: Sie erzeugt keine Anmeldung und wechselt keine bestehende Sitzung, auch bei Auswahl eines Admins. Bei diesen ohne Sitzung anlegbaren Beiträgen nennt das Audit der Neuanlage den gewählten Ersteller. Zum Kommentieren, Bewerten und Bearbeiten wählt man weiterhin über **Namen wählen** den eigenen Namen; Audit und Verwaltung behalten ihre Rollenprüfungen und den separaten Admin-Zugang. Bei Basiseinträgen, Ideen, Werkzeugmeldungen und Anschaffungswünschen können alle aktiven Autorenrollen ausgewählt werden; Rechte für spätere Bearbeitungen entstehen daraus nicht automatisch. Das Anlegen und Zuweisen einer Aufgabe sowie das Anlegen und Pflegen des Werkzeugbestands erfordern dagegen eine aktive Vorgesetzten- oder Admin-Sitzung. Für neue Aufgaben und Werkzeuge wird auch ein entsprechend berechtigter Ersteller gewählt; Audit-Akteur bleibt der tatsächlich handelnde Sitzungsnutzer.

## Ideenboard und Prioritäten

Über **Ideenboard** (`/ideas`) neue Ideen mit Titel, Beschreibung und Kategorie vorschlagen. Kategorien sind vorerst frei benannte Pflichtfelder mit höchstens 100 Zeichen; bereits verwendete Kategorien werden vorgeschlagen. Der Startwert im Formular ist „Allgemein“. Eine zentrale Kategorienverwaltung folgt in Meilenstein 7.

Beim Anlegen einer Idee ist wie bei einem Basiseintrag der Ersteller ausdrücklich auszuwählen. Eine Idee beginnt als **Neu**. Vorgesetzte und Admins können den Status auf **In Prüfung**, **Geplant**, **Umgesetzt** oder **Abgelehnt** setzen. Autor und ursprüngliche Signatur bleiben erhalten. Titel, Beschreibung, Kategorie, Status und Prioritäten verwenden dieselbe Versionsprüfung, damit veraltete Formulare keine inzwischen erfolgte Änderung überschreiben.

Beide Prioritäten sind ohne Anmeldung im Werkstattnetz sichtbar und haben dieselbe konfigurierbare Skala. Größere Zahlen bedeuten höhere Priorität. Die Werte können auch bei den bisherigen Basiseinträgen gesetzt werden:

- **Mitarbeiter-Priorität:** Mitarbeiter dürfen sie bei allen Einträgen setzen. Der jeweilige Ersteller darf sie unabhängig von seiner Rolle bei eigenen Einträgen setzen. Vorgesetzte und Admins dürfen den Mitarbeiterwert anderer Ersteller nicht überschreiben.
- **Vorgesetzten-Priorität:** Vorgesetzte und Admins dürfen sie bei allen Einträgen setzen.
- Das sind zwei **gemeinsam gepflegte Werte**, keine Abstimmung pro Nutzer. Ein berechtigter Nutzer kann den jeweiligen Wert ersetzen oder mit „Noch nicht gesetzt“ löschen. Eine tatsächliche Änderung erhöht die Version und speichert Akteur, Zeitpunkt und vorherigen/neuen Stand atomar im Audit-Log. Unverändertes Speichern erzeugt keinen zusätzlichen Audit-Eintrag.
- Eine **starke Abweichung** wird als Symbol und Text markiert, sobald beide Werte gesetzt sind und ihre Differenz mindestens `(Maximum − Minimum) / 2 + 1` beträgt (ganzzahlig abgerundet vor `+ 1`). Bei der Standardskala 1–5 sind das mindestens drei Stufen.

Das Board filtert nach Kategorie, Status, beiden Prioritäten (auch „Noch nicht gesetzt“) und starken Abweichungen. Alle gesetzten Filter werden kombiniert. Sortierung nach Mitarbeiter- oder Vorgesetzten-Priorität ist absteigend; unbewertete Ideen stehen am Ende. Bei Gleichstand erscheinen neuere Ideen zuerst.

Die Skala wird über `priority_min` und `priority_max` in der lokalen JSON-Konfiguration geändert. Die App dazu neu starten. Fehlen diese Angaben in einer bisherigen Konfiguration, gilt weiterhin 1–5. Eine Skala, die bereits gespeicherte Werte ausschließt, führt zu einem verständlichen Startfehler; bestehende Werte werden nicht umgerechnet oder gelöscht.

## Aufgabenboard und Erinnerungen

Das **Aufgabenboard** (`/tasks`) zeigt die vier Kanban-Status **Offen**, **In Arbeit**, **Zur Kontrolle** und **Erledigt**. Gesamtboard, Aufgabendetails, beide Prioritäten und Kommentare sind im Werkstattnetz ohne Anmeldung lesbar. Nach optionaler Namensauswahl führt **Meine Aufgaben** (`/tasks?mine=1`) ausschließlich zu den Aufgaben der gewählten Person. Das Gesamtboard kann nach zugewiesener Person und Status gefiltert werden; Aufgaben sind nach Fälligkeit und bei gleichem Datum nach neuestem Eintrag geordnet.

**Zuweisung und Abnahme:** Vorgesetzte und Admins legen Aufgaben mit Titel, Beschreibung, zugewiesener Person und verpflichtendem Fälligkeitsdatum an. Der Ersteller wird auch hier ausdrücklich ausgewählt, jedoch aus den aktiven Vorgesetzten und Admins. Diese Auswahl ändert keine Sitzung. Die ursprüngliche Signatur nennt den gewählten Ersteller; das Audit nennt den tatsächlich handelnden Vorgesetzten oder Admin aus der Sitzung. Beide Personen können verschieden sein. Ein ausgewählter Ersteller ersetzt daher keine für die Zuweisung notwendige Berechtigung.

Es gibt noch keine eigene Azubi-Kennzeichnung: Aufgaben können zunächst allen **aktiven Mitarbeitern** zugewiesen werden, einschließlich Azubis. Ausbilder verwenden die Rolle **Vorgesetzter**. Eine Aufgabe beginnt mit **Offen**. Die zugewiesene Person darf zwischen den ersten drei Status wechseln; andere Mitarbeiter dürfen ihren Arbeitsstatus nicht ändern. Titel, Beschreibung, Fälligkeit und Zuweisung ändern ausschließlich Vorgesetzte und Admins. Eine neue Zuweisung setzt den Status auf **Offen** zurück.

Eine Aufgabe kann erst aus **Zur Kontrolle** durch einen Vorgesetzten oder Admin **abgenommen** und damit **Erledigt** werden. Die Abnahme speichert zusätzlich Namen, Nutzer-ID und Serverzeit des Abnehmenden. Erledigte Aufgaben müssen vor Änderungen an Text, Fälligkeit oder Zuweisung wieder geöffnet werden. Beim Wiederöffnen wird die aktuelle Abnahmesignatur entfernt; frühere Abnahmen bleiben im Audit erhalten. Die ursprüngliche Erstellersignatur bleibt immer unverändert. Kommentare und Prioritäten nutzen weiterhin die gemeinsamen Rechte und die gemeinsame Änderungshistorie. Status, Zuweisung, Inhalt und Prioritäten teilen sich die Versionsprüfung gegen veraltete Formulare.

**Erinnerungen** erscheinen beim Öffnen des Dashboards und in der eigenen Aufgabenansicht für alle noch nicht erledigten Aufgaben:

- **Überfällig:** Fälligkeitsdatum liegt vor dem heutigen Werkstatttag.
- **Heute fällig:** Fälligkeitsdatum ist der heutige Werkstatttag.
- **Bald fällig:** Fälligkeitsdatum liegt innerhalb der nächsten `reminder_days` Kalendertage, einschließlich des letzten Tages; standardmäßig sind das drei Tage.

Ohne Namenssitzung sowie für Vorgesetzte und Admins zeigt das Dashboard die allgemeinen Aufgabenerinnerungen. Bei einer Mitarbeiter-Sitzung erscheinen dort die eigenen Aufgaben und Erinnerungen. Es werden keine E-Mails oder Push-Nachrichten versendet. Die Anzeige wird beim Seitenaufruf berechnet; ein separater Zeitplaner ist dafür nicht nötig.

Fälligkeiten sind reine Kalenderdaten im Format `YYYY-MM-DD`. Für Tagesgrenzen gilt `timezone`, standardmäßig **Europe/Berlin**; Sommerzeitwechsel werden als Kalenderwechsel behandelt. Die Zeitzonendaten sind im Programm eingebettet, damit auch dafür im Pi-Betrieb kein Internetzugriff nötig ist. Signatur- und Audit-Zeiten werden weiterhin in UTC gespeichert und angezeigt.

## Werkzeugbestand, Meldungen und Anschaffung

Der **Werkzeugbestand** (`/tools`) zeigt Name, Kategorie, Standort und den offiziellen Zustand **OK**, **Verschleißt**, **Defekt** oder **Ersetzt**. Die Übersicht lässt sich nach Zustand und Kategorie filtern und nach Namen durchsuchen. Werkzeuge werden von Vorgesetzten und Admins angelegt und gepflegt. Beim Anlegen ist ein aktiver Vorgesetzter oder Admin als Ersteller zu wählen; dessen Name und Serverzeit bilden die ursprüngliche Signatur. Im Audit steht der handelnde Sitzungsnutzer. Ein ersetztes Werkzeug bleibt für seine Historie im Bestand; ein Ausleihsystem und QR-Etiketten gehören noch nicht zu diesem Modul.

Über **Zustand melden** am Werkzeug können alle Nutzer ohne vorherige Anmeldung einen Beitrag mit Titel, Beschreibung, beobachtetem Zustand und ausdrücklich gewähltem aktivem Ersteller absenden. Die Meldung ist signiert und beginnt als **Neu**. Sie hat eine eigene Detailansicht (`/tool-reports/{id}`), Kommentare und beide Prioritäten. Mitarbeiter bearbeiten ihre eigenen Meldungstexte und Beobachtungen, Vorgesetzte und Admins alle Meldungen. Den Bearbeitungsstatus **Neu**, **In Prüfung** oder **Erledigt** ändern nur Vorgesetzte und Admins.

Eine Meldung verändert den **offiziellen Werkzeugzustand nicht automatisch**. Offene Meldungen (**Neu** oder **In Prüfung**) werden am Werkzeug und im Dashboard sichtbar markiert, auch wenn der gepflegte Zustand noch „OK“ lautet. Vorgesetzte und Admins prüfen die Meldung, pflegen den offiziellen Zustand und schließen die Meldung ausdrücklich ab. Werkzeugzustand und Meldungsstatus bleiben getrennt nachvollziehbar.

**Anschaffungswünsche** (`/purchases`) können ebenfalls ohne vorherige Anmeldung mit aktiver Erstellerwahl erfasst werden. Titel, Beschreibung und Kategorie sind Pflichtfelder; die Kategorie startet im Formular als „Allgemein“. Ein Zielstandort und ein Bezug zu einem vorhandenen Werkzeug, etwa für eine Ersatzbeschaffung, sind optional. Bei einer neuen Anschaffung ist kein Werkzeugbezug erforderlich. Der einmal gewählte Werkzeugbezug einer Meldung oder eines Wunsches bleibt unveränderlich, damit die Historie zuverlässig zugeordnet bleibt.

Ein Wunsch beginnt als **Gewünscht**. Vorgesetzte und Admins setzen den Beschaffungsstatus **Gewünscht**, **Genehmigt**, **Bestellt** oder **Geliefert**; auch Korrekturen zwischen diesen Status sind erlaubt und werden auditiert. Mitarbeiter ändern Texte, Kategorie und Zielstandort ihrer eigenen Wünsche nur, solange der Wunsch **Gewünscht** ist. Danach sind diese Änderungen Vorgesetzten und Admins vorbehalten. „Geliefert“ legt keinen Werkzeugbestand automatisch an: Ein tatsächlich eingetroffenes Werkzeug wird mit seinem Namen, Standort und Zustand separat erfasst.

Meldungen und Wünsche verwenden die gemeinsame Prioritätsskala, unabhängige Mitarbeiter-/Vorgesetztenwerte, Kommentare, unveränderliche Signaturen und Versionsprüfung. Filter und Sortierung berücksichtigen beide Prioritäten, einschließlich unbewerteter Einträge und starker Abweichungen. Jede tatsächliche Änderung wird atomar mit Akteur, Zeitpunkt und vorherigem/neuem Stand gespeichert; unverändertes Speichern erzeugt kein zusätzliches Audit-Ereignis.

Die **Historie pro Werkzeug** zeigt ausschließlich zugehörige Änderungen am Werkzeug, verknüpfte Meldungen und Anschaffungswünsche sowie deren Kommentare. Diese Historie ist im Werkstattnetz öffentlich lesbar. Das vollständige Audit-Log mit Benutzerverwaltung und anderen Bereichen bleibt Vorgesetzten und Admins vorbehalten.

## Rechte und weitere Annahmen

Lesen sowie die Neuanlage von Basiseinträgen, Ideen, Werkzeugmeldungen und Anschaffungswünschen mit Erstellerwahl sind wie oben beschrieben ohne Sitzung möglich. Für weitere Aktionen gelten in einer aktiven Namenssitzung folgende Rechte; die ersten Status- und Textregeln beziehen sich auf Basiseinträge und Ideen:

| Aktion | Mitarbeiter (inkl. Azubi) | Vorgesetzter | Admin |
| --- | --- | --- | --- |
| Beiträge kommentieren | Ja | Ja | Ja |
| Titel, Text und Ideenkategorie ändern | Eigene Einträge | Alle | Alle |
| Status ändern | Nein | Ja | Ja |
| Mitarbeiter-Priorität setzen/löschen | Alle Einträge | Eigene Einträge | Eigene Einträge |
| Vorgesetzten-Priorität setzen/löschen | Nein | Alle Einträge | Alle Einträge |
| Aufgaben zuweisen, Inhalt/Fälligkeit ändern | Nein | Ja | Ja |
| Aufgabenstatus vor Abnahme ändern | Nur zugewiesene Aufgaben | Alle | Alle |
| Aufgaben abnehmen und wieder öffnen | Nein | Ja | Ja |
| Werkzeugbestand und offiziellen Zustand pflegen | Nein | Ja | Ja |
| Werkzeugmeldungen ändern | Eigene Texte und Beobachtung | Alle, einschließlich Status | Alle, einschließlich Status |
| Anschaffungswünsche ändern | Eigene, solange „Gewünscht“ | Alle, einschließlich Status | Alle, einschließlich Status |
| Vollständiges Audit-Log ansehen | Nein | Ja | Ja |
| Benutzer anlegen, umbenennen, deaktivieren, Rollen setzen | Nein | Nein | Ja |

- Die optionale Namenssitzung und die Erstellerwahl verwenden weiterhin **keine Passwörter/PINs**, auch auf der separaten Admin-Anmeldeseite. Sie benennen die gewählte Person, prüfen ihre Identität jedoch nicht. Wer das Werkstattnetz erreicht, kann auch einen Admin-Namen auswählen. Nur in diesem vertrauten lokalen Netz betreiben. Eine Signatur ist Name + Serverzeit, keine kryptografische Unterschrift.
- Rollenprüfungen erfolgen serverseitig. Inaktive Nutzer erscheinen weder in der Erstellerwahl noch in der Anmeldung. Bestehende Sitzungen berücksichtigen Deaktivierungen und Rollenänderungen sofort; öffentliches Lesen bleibt möglich. Authentifizierung und Sitzungen sind im Webpaket gekapselt und können später um PIN/Passwort ergänzt werden. Sitzungen liegen nur im Arbeitsspeicher, laufen ab und enden bei Neustart.
- Jeder Basiseintrag beginnt mit Status `neu`. Weitere Basisstatus: `in_pruefung`, `geplant`, `erledigt`, `abgelehnt`. Ideen, Aufgaben, Werkzeugmeldungen und Anschaffungswünsche verwenden die oben beschriebenen eigenen Statusabläufe.
- Signaturen und Autor bleiben unverändert, auch nach einer Benutzerumbenennung. Kommentare sind nach dem Absenden unveränderlich. Signatur- und Audit-Zeiten werden in UTC gespeichert und angezeigt; Aufgabenfälligkeiten verwenden lokale Kalenderdaten.
- Änderungen an Einträgen, Kommentare und Benutzerverwaltung werden atomar mit dem Audit-Ereignis gespeichert: Akteur als ID und damaliger Name, Serverzeit, Aktion und vollständiger vorheriger/neuer Zustand. Bei einem Audit-Fehler wird die gesamte Änderung zurückgerollt. Parallele Eintragsbearbeitungen erkennen veraltete Versionen und überschreiben diese nicht.
- Das Audit-Log ist über App und Datenbank-Trigger nur erweiterbar. Die gemeinsame Anwendungsschicht schützt alle Schreibvorgänge; direkter Zugriff auf die SQLite-Datei gehört zum vertrauenswürdigen Betrieb und ist kein unterstützter Schreibweg.
- Eine aktive Admin-Person muss erhalten bleiben. Der letzte aktive Admin kann weder deaktiviert noch heruntergestuft werden.
- Ideen, Aufgaben, Werkzeuge, Meldungen, Anschaffungswünsche und generische Basiseinträge einschließlich Kommentaren sind ohne Anmeldung im Werkstattnetz sichtbar. Material- und Kummerkastenmodule folgen noch. Vertrauliche Kummerkasten-Inhalte erst im entsprechend geschützten späteren Modul erfassen.

## Bestehende Installation aktualisieren

Bei einem Update einer bestehenden Installation zuerst die App stoppen und das vollständige Datenverzeichnis sichern. Dann Branch `alpha` aktualisieren, das neue Programm bauen bzw. mit `go run` starten und dieselbe Konfiguration sowie Datenbank verwenden. **`init` nicht erneut ausführen.** Fehlende versionierte Migrationen werden beim Start automatisch angewendet: Migration 002 ergänzt Ideen/Prioritäten, Migration 003 das Aufgabenboard und die neue Migration 004 die Werkzeugverwaltung. IDs, Nutzer, Basis- und Ideeneinträge, Aufgaben einschließlich Abnahmen, Signaturen, Kommentare, Prioritäten und Audit-Historie aus Meilensteinen 1 bis 3 bleiben erhalten. Bereits angewendete Migrationen 001 bis 003 werden nicht verändert. Meilenstein 4 benötigt keine neuen Konfigurationswerte; alte Konfigurationen übernehmen für bisher fehlende Angaben `timezone` und `reminder_days` automatisch die Standardwerte. Eine bereits aktualisierte Datenbank benötigt mindestens diese Programmversion; für eine Rückkehr zum alten Programm die vorherige Sicherung verwenden.

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
internal/tasks/                Aufgaben, Statusrechte und Kalender-Erinnerungen
internal/tooling/              Werkzeugbestand, Zustandsmeldungen und Anschaffung
internal/storage/              SQLite, Transaktionen und Audit-Log
internal/storage/migrations/   Versioniertes Schema
internal/web/                  HTTP, Sitzungen, Templates, lokale Assets
```

Tests verwenden ausschließlich temporäre Verzeichnisse. Datenbanken, Uploads, Sicherungen, Logs, lokale Konfiguration und Geheimnisse dürfen nicht versioniert werden; `.gitignore` schützt die üblichen Pfade und Dateiformate. Beispielkonfiguration enthält ausschließlich lokale Standardwerte. Änderungen ausschließlich auf Branch `alpha`, in kleinen thematischen Commits; niemals direkt auf `main` pushen.

Ideenboard, Aufgabenboard und Werkzeugverwaltung sind eigene Fachpakete und nutzen die gemeinsame Benutzer-, Signatur-, Kommentar-, Prioritäts- und Audit-Basis. Als nächster Schritt folgen Verbrauchsmaterialien mit Bestandswarnungen und Abhaken (Meilenstein 5), danach Kummerkasten, erweiterte Administration/Uploads und Pi-Betrieb gemäß `AGENTS.md`.

Für Version **0.4.0** sind `go test -race ./...`, `go vet ./...` sowie Builds ohne CGO für Linux/AMD64 und Linux/ARM64 erfolgreich. Die Tests prüfen Rollen und aktive Ersteller, unveränderliche Signaturen, die Trennung von Werkzeugzustand und Meldung, Beschaffungsrechte, vollständige Prioritäts-Snapshots, begrenzte öffentliche Historien, Audit-Rollback und konkurrierende Bearbeitungen. Migration 004 ist mit bestehenden Aufgaben einschließlich signierter Abnahme, Kommentaren, Prioritäten und unveränderten Audit-Bytes geprüft.

Der vollständige Chromium-Ablauf deckt Werkzeuganlage, Zustandsmeldung, offizielle Zustandspflege, Kommentieren/Priorisieren, Anschaffung bis zur Lieferung, Historienbegrenzung sowie Filter, Sortierung und Suche bei 320, 390, 768 und 1360 Pixeln ab. Dashboard und neue Meldungen/Wünsche sind direkt erreichbar; Erstellerwahl erzeugt keine Sitzung. Alle geladenen Ressourcen sind lokal. Zusätzlich wurde eine bestehende Meilenstein-3-Testdatenbank aktualisiert und der unveränderte Inhalt der bisherigen Tabellen verglichen. Getestete Toolchain: Go 1.27.4. Der ARM64-Build wurde noch nicht auf echter Pi-Hardware ausgeführt.
