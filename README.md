# Werkstatt-Hub

Lokale Webapp für eine KFZ-Werkstatt. Meilensteine 1 bis 5 und die Erweiterung **Werkstattwartung** sind umgesetzt: direkt zugängliches Dashboard, Erstellerwahl pro neuem Beitrag, passwortgeschützter Admin-Zugang, Rollen, signierte Beiträge mit Änderungshistorie, Ideenboard mit zwei unabhängigen Prioritäten, Aufgabenboard mit Fälligkeiten und Abnahme, Werkzeugbestand, Zustandsmeldungen und Anschaffungswünsche, Verbrauchsmaterialien mit Bestandswarnungen sowie regelmäßige Wartungen mit unveränderlicher Historie. Oberfläche und Fehlermeldungen sind deutsch.

## Tech-Stack

**Go + SQLite + HTML-Templates**, ohne SPA-Framework. Go liefert einen ressourcenschonenden HTTP-Server und ein einzelnes ausführbares Programm; SQLite benötigt keinen eigenen Datenbankdienst und reicht für etwa zwölf Nutzer bequem aus. Der reine Go-Treiber `modernc.org/sqlite` ermöglicht Builds für Raspberry Pi ohne C-Toolchain. Templates, CSS und Migrationen sind eingebettet. Das hält Installation und Wartung einfach. Im Betrieb gibt es keine externen Dienste, CDNs, Fonts, JavaScript-Abhängigkeiten oder Telemetrie.

## Lokal starten

Voraussetzung: **Go 1.26 oder neuer**. Für das erstmalige Laden der versionierten Go-Abhängigkeiten braucht der Entwicklungsrechner Internet. Die fertige App läuft offline.

```sh
git clone --branch alpha https://github.com/karst3nl/Werkstatt-Hub.git
cd Werkstatt-Hub
go mod download
go run ./cmd/werkstatt-hub serve
```

Öffnen: **http://127.0.0.1:8080**. Das Dashboard erscheint direkt ohne Anmeldung. Beim ersten Serverstart wird das Konto **Admin** automatisch angelegt; sein zufälliges **Startpasswort** steht einmalig in der lokalen Serverkonsole. Über **Admin** (`/admin/login`) mit diesem Passwort anmelden und anschließend ein neues Passwort festlegen. Erst danach ist die Verwaltung freigeschaltet. Unter **Benutzerverwaltung** kann der Admin Mitarbeiter und Vorgesetzte anlegen. Es werden keine Beispielbeiträge angelegt. Ein vorheriger `init`-Aufruf ist nicht erforderlich.

Standardmäßig liegen sämtliche Laufzeitdaten **außerhalb des Checkouts** unter `../werkstatt-hub-data/werkstatt-hub.db`. Alle relativen Pfade beziehen sich auf das aktuelle Arbeitsverzeichnis, nicht auf den Ort der Konfigurationsdatei. SQLite erzeugt bei laufendem Betrieb gegebenenfalls zusätzliche WAL-/SHM-Dateien im selben Verzeichnis.

## Konfiguration und Build

Ohne Konfigurationsdatei gelten die Werte aus `config.example.json`. Für eigene Werte:

```sh
cp config.example.json config.local.json
go run ./cmd/werkstatt-hub serve --config config.local.json
```

| Feld | Bedeutung |
| --- | --- |
| `listen` | Standard `127.0.0.1:8080`; für das Werkstattnetz z. B. `0.0.0.0:8080` |
| `database` | Pfad zur lokalen SQLite-Datei |
| `session_ttl` | Sitzungsdauer als Go-Dauer, Standard `12h`, maximal `168h` |
| `secure_cookies` | Für lokales HTTP `false`; bei späterem HTTPS `true` |
| `priority_min`, `priority_max` | Gemeinsame Skala für beide Prioritäten; Standard 1–5, zulässig `1 ≤ Minimum < Maximum ≤ 100` |
| `timezone` | Werkstatt-Zeitzone für Aufgaben- und Wartungsfälligkeiten, Standard `Europe/Berlin` |
| `reminder_days` | Zeitraum für „bald fällig“ in Kalendertagen, Standard `3`, zulässig 1–30 |

```sh
CGO_ENABLED=0 go build -o bin/werkstatt-hub ./cmd/werkstatt-hub
./bin/werkstatt-hub serve --config config.local.json
# Raspberry Pi mit 64-Bit-Linux, auf dem Entwicklungsrechner gebaut:
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/werkstatt-hub-linux-arm64 ./cmd/werkstatt-hub
```

Für ein späteres Deployment werden nur das passende Binary, eine lokale Konfiguration und ein beschreibbares Datenverzeichnis benötigt. Auf dem Pi einen festen Arbeitsordner oder absolute Datenbankpfade verwenden. Das Programm beendet HTTP-Verbindungen bei SIGINT/SIGTERM geordnet. systemd-Service und automatisierte lokale/NAS-Backups folgen in Meilenstein 8. Bis dahin zum Sichern die App stoppen und das vollständige Datenverzeichnis kopieren.

## Admin-Zugang und Nutzerverwaltung

Bei der ersten Einrichtung sowie für bestehende aktive Admins ohne Passwort erzeugt der Server jeweils ein zufälliges Startpasswort. Es gibt kein universelles Standardpasswort im Repository. Die einmalige Ausgabe erfolgt nur bei der lokalen Einrichtung; ein Neustart setzt vorhandene Passwörter nicht zurück und gibt sie nicht erneut aus. Das Startpasswort bis zum Abschluss der Einrichtung bereithalten.

Nach der ersten Passwortanmeldung ist der Wechsel unter `/admin/password` verpflichtend. Das neue Passwort muss sich vom bisherigen unterscheiden und 12 bis 128 Zeichen bei höchstens 512 UTF-8-Bytes haben. Bis zum Wechsel sind geschützte Aktionen gesperrt; das öffentlich lesbare Dashboard bleibt erreichbar. Passwortwechsel und Zurücksetzen machen bisherige Sitzungen ungültig. Die bloße Namensauswahl oder eine frühere Mitarbeitersitzung gewährt keine Admin-Rechte.

**Nur ein vollständig angemeldeter Admin** kann Nutzer anlegen, ändern, deaktivieren, löschen und Rollen vergeben. Bei einem neuen Admin oder einer Beförderung zum Admin legt der handelnde Admin ein temporäres Passwort fest; das Zielkonto muss es nach der Anmeldung ändern. Bei bestehenden Admins kann ein temporäres Passwort zum Zurücksetzen gesetzt werden. Der letzte aktive Admin kann weder gelöscht noch deaktiviert oder heruntergestuft werden.

**Nutzer löschen** entfernt die Person aus Benutzer- und Erstellerlisten sowie der Anmeldung und beendet ihre Sitzungen. Der historische Nutzerdatensatz bleibt als gelöschter, inaktiver Datensatz bestehen, damit Einträge, Signaturen, Abnahmen und Audit-Verweise erhalten bleiben. Ein später neu angelegter gleichnamiger Nutzer erhält eine eigene ID. Es gibt keine Selbstregistrierung oder allgemeine Profilbearbeitung.

Admin-Passwörter werden ausschließlich als gesalzene PBKDF2-SHA256-Hashes mit 600.000 Iterationen in der lokalen Datenbank gespeichert. Passwörter und Hashes werden nicht in Audit-Einträge, Benutzeransichten oder URLs übernommen. Die bewusste einmalige Startpasswortausgabe in der lokalen Serverkonsole ist für die Einrichtung vorgesehen. Mitarbeiter und Vorgesetzte nutzen weiterhin die optionale Namensauswahl ohne Passwort.

## Dashboard und Erstellerwahl

Die Startseite öffnet unmittelbar das **Werkstatt-Dashboard**, ohne Anmeldefenster. Auf breiten Bildschirmen stehen Aufgaben links, Werkzeuge und Anschaffungen in der Mitte und Ideen rechts nebeneinander. Auf schmalen Bildschirmen folgen diese Bereiche in derselben Reihenfolge untereinander. Offene Bestandswarnungen für Verbrauchsmaterialien stehen über dem Wartungsblock und den drei Spalten. Ideenboard, Aufgabenboard, Werkzeugbestand, Zustandsmeldungen, Anschaffungswünsche, Verbrauchsmaterialien und Bestandsprüfungen, deren Detailansichten und vorhandene Kommentare können im Werkstattnetz ohne Namenssitzung gelesen werden. Auch nach Abmeldung, abgelaufener Sitzung oder Deaktivierung eines zuvor gewählten Namens bleibt das Dashboard erreichbar.

Bei **jeder neuen Idee, jeder Werkzeugmeldung und jedem Anschaffungswunsch** ist im Formular ein aktiver **Ersteller** aus der Namensliste auszuwählen. Die Auswahl beginnt leer, auch bei einer bestehenden Sitzung. Name, Autor-ID und Signaturzeit werden serverseitig aus diesem Nutzer und der aktuellen Uhrzeit erzeugt. Freie Signatur- oder Zeitangaben aus dem Browser werden nicht übernommen. Fehlende, unbekannte, deaktivierte und gelöschte Ersteller werden abgewiesen.

Die Erstellerwahl gilt ausschließlich für den neuen Beitrag: Sie erzeugt keine Anmeldung und wechselt keine bestehende Sitzung, auch bei Auswahl eines Admins. Bei diesen ohne Sitzung anlegbaren Beiträgen nennt das Audit der Neuanlage den gewählten Ersteller. Zum Kommentieren, Bewerten und Bearbeiten wählen Mitarbeiter und Vorgesetzte über **Namen wählen** den eigenen Namen; Admins verwenden ihren Passwortzugang. Audit und Verwaltung behalten ihre Rollenprüfungen. Bei Ideen, Werkzeugmeldungen und Anschaffungswünschen können alle aktiven Autorenrollen ausgewählt werden; Rechte für spätere Bearbeitungen entstehen daraus nicht automatisch. Das Anlegen und Zuweisen einer Aufgabe sowie das Anlegen und Pflegen des Werkzeugbestands erfordern dagegen eine aktive Vorgesetzten- oder Admin-Sitzung. Für neue Aufgaben und Werkzeuge wird auch ein entsprechend berechtigter Ersteller gewählt; Audit-Akteur bleibt der tatsächlich handelnde Sitzungsnutzer.

## Ideenboard und Prioritäten

Über **Ideenboard** (`/ideas`) neue Ideen mit Titel, Beschreibung und Kategorie vorschlagen. Kategorien sind vorerst frei benannte Pflichtfelder mit höchstens 100 Zeichen; bereits verwendete Kategorien werden vorgeschlagen. Der Startwert im Formular ist „Allgemein“. Eine zentrale Kategorienverwaltung folgt in Meilenstein 7.

Beim Anlegen einer Idee ist der Ersteller ausdrücklich auszuwählen. Eine Idee beginnt als **Neu**. Vorgesetzte und Admins können den Status auf **In Prüfung**, **Geplant**, **Umgesetzt** oder **Abgelehnt** setzen. Autor und ursprüngliche Signatur bleiben erhalten. Titel, Beschreibung, Kategorie, Status und Prioritäten verwenden dieselbe Versionsprüfung, damit veraltete Formulare keine inzwischen erfolgte Änderung überschreiben.

Beide Prioritäten sind ohne Anmeldung im Werkstattnetz sichtbar und haben dieselbe konfigurierbare Skala. Größere Zahlen bedeuten höhere Priorität. Die gemeinsame Darstellung zeigt immer **fünf lokal ausgelieferte Schraubenschlüssel**: Bei der Standardskala 1–5 sind entsprechend viele Symbole gefüllt, von Grün über Hellgrün, Gelb und Orange bis Rot. Übrige Symbole bleiben als graue Umrisse sichtbar; ohne Bewertung werden fünf graue Umrisse und „Noch nicht gesetzt“ angezeigt. Zugängliche Beschriftungen nennen den ursprünglichen Wert und die Skala. Bei anderen konfigurierten Skalen wird nur die Darstellung auf fünf Stufen abgebildet; gespeicherte Werte, Filter und Audit bleiben unverändert. Die Eingabefelder dürfen weiterhin numerische Werte verwenden.

Für das Setzen und Löschen gelten folgende Rechte:

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

## Verbrauchsmaterialien und Bestandswarnungen

Der Reiter **Verbrauchsmaterialien** (`/materials`) zeigt Name, Einheit, Lagerort, aktuellen Bestand und Mindestbestand. Die Übersicht lässt sich nach Namen, aktiven/deaktivierten Materialien und niedrigem Bestand filtern. Materialdetails (`/materials/{id}`), Bestandsprüfungen und Warnungshistorie sind ohne Anmeldung im Werkstattnetz lesbar. Ein Bestand auf oder unter dem Mindestbestand wird als niedrig gekennzeichnet; eine offene Bestellwarnung ist davon getrennt sichtbar.

**Stammdaten:** Nur ein vollständig angemeldeter Admin legt Materialien an (`/admin/materials/new`) und bearbeitet Name, Lagerort, Mindestbestand sowie Aktivierung (`/admin/materials/{id}/edit`). Beim Anlegen werden Einheit und Anfangsbestand festgelegt und mit dem handelnden Admin signiert. Die Einheit bleibt danach unveränderlich, damit historische und aktuelle Mengen ihre Bedeutung behalten. Weitere Bestandsänderungen werden ausschließlich als neue Bestandsprüfung erfasst. Materialien werden deaktiviert statt gelöscht; Historie und offene Warnungen bleiben erhalten. Deaktivierte Materialien nehmen keine neuen Prüfungen an und erscheinen nicht in den Dashboard-Warnungen. Nach Reaktivierung wird eine noch offene Warnung wieder angezeigt.

**Bestand prüfen:** Mitarbeiter einschließlich Azubis, Vorgesetzte und Admins können den tatsächlich gezählten Bestand mit optionaler Notiz eintragen. Dafür wird bei jeder Prüfung ausdrücklich ein aktiver Ersteller aus der Namensliste gewählt, auch bei einer vorhandenen Sitzung. Die Auswahl beginnt leer und erzeugt oder wechselt keine Sitzung. Der gewählte Nutzer, Name und Serverzeit bilden die unveränderliche Signatur und den Audit-Akteur. Prüfungen werden zum Erfassungszeitpunkt gespeichert; ein frei gewähltes vergangenes oder zukünftiges Prüfdatenfeld gibt es nicht. Die Versionsprüfung des Materials weist veraltete Formulare mit einem Konflikt zurück, damit eine zwischenzeitliche Zählung nicht still überschrieben wird.

Mengen sind nichtnegative Werte von **0 bis 999999999,999** mit höchstens drei Nachkommastellen. Komma oder Punkt ist als Dezimaltrenner zulässig; angezeigt wird das deutsche Kommaformat. Die Speicherung verwendet ganze Tausendstel ohne Fließkommarundung. Negative Mengen, Exponentialschreibweise und Werte mit mehr Nachkommastellen werden abgewiesen. Das gilt für Bestand und Mindestbestand gleichermaßen.

Jede Prüfung hat eine eigene Detailansicht (`/material-checks/{id}`), Status **Erfasst**, beide gemeinsamen Prioritäten mit fünf Schraubenschlüsseln und Kommentare. Die Prüfungshistorie am Material lässt sich nach beiden Prioritäten einschließlich unbewerteter Prüfungen filtern und nach neuesten Prüfungen oder der höchsten Mitarbeiter-/Vorgesetztenpriorität sortieren. Prüfmengen, ursprüngliche Notiz, Materialbezug und Signatur werden nicht nachträglich bearbeitet oder gelöscht. Eine Korrektur erfolgt durch eine neue Bestandsprüfung; Erläuterungen können als Kommentar ergänzt werden. Kommentare und Prioritätsänderungen benötigen die bestehende Namens-/Admin-Sitzung und folgen derselben Rechtematrix wie die anderen Beiträge. Frühere Signaturen bleiben auch nach Nutzerumbenennung oder -löschung erhalten.

Für den Warnungsablauf gelten folgende Annahmen:

- Ein Anfangsbestand auf oder unter dem Mindestbestand erzeugt bereits eine Warnung. Später lösen ein Rückgang auf oder unter das Minimum sowie eine Mindestbestandserhöhung, durch die der aktuelle Bestand niedrig wird, eine Warnung aus.
- Pro Material gibt es höchstens eine offene Warnung. Weitere niedrige Prüfungen erzeugen währenddessen keine Duplikate. Der auslösende Bestand und Mindestbestand bleiben in der Warnung nachvollziehbar.
- Eine offene Warnung verschwindet erst durch **Abhaken** aus einer aktiven Vorgesetzten-/Admin-Sitzung. Die Signatur nennt den handelnden Sitzungsnutzer und den Serverzeitpunkt; eine Erstellerwahl ersetzt diese Berechtigung nicht. Eine Auffüllung allein hakt die Warnung nicht ab.
- Nach dem Abhaken erzeugt derselbe oder ein gestiegener niedriger Bestand keine neue Warnung. Sinkt der Bestand später erneut und liegt dabei auf oder unter dem Minimum, beginnt ein neuer Warnungszyklus. Der frühere Zyklus mit Bestätigung bleibt in der Historie.
- Abhaken dokumentiert die Bearbeitung der Bestellwarnung. Es verändert den Bestand nicht; tatsächlich eingetroffenes Material wird durch eine neue Zählung erfasst. Daher kann ein Material nach dem Abhaken weiterhin als „niedriger Bestand“ gelistet sein.

Offene Warnungen aktiver Materialien stehen mit „… sollte bestellt werden“ ganz oben im Dashboard. Vorgesetzte und Admins können sie dort oder am Material signiert abhaken. Alle Material-, Prüf-, Warnungs- und Bestätigungsänderungen werden zusammen mit ihrem Audit-Eintrag gespeichert; bei einem Fehler wird die gesamte Änderung zurückgerollt. Prüfintervalle und automatisch erzeugte Azubi-Aufgaben bleiben eine spätere optionale Erweiterung. Es gibt keine externen Dienste oder Benachrichtigungen.

## Werkstattwartung

Der Reiter **Werkstattwartung** (`/wartung`) zeigt regelmäßig zu wartende Objekte, nach nächster Fälligkeit sortiert. Übersicht und Details einschließlich Wartungshistorie sind ohne Anmeldung im Werkstattnetz lesbar. Kategorie, Fälligkeitsstatus und aktive/deaktivierte Objekte lassen sich filtern. **In Ordnung**, **Bald fällig** und **Überfällig** werden mit Text und grüner, gelber bzw. roter Kennzeichnung dargestellt. Das Dashboard zeigt darüber einen eigenen Wartungsblock mit den Anzahlen bald fälliger und überfälliger Objekte; überfällige Wartungen erscheinen oberhalb der bisherigen drei Spalten.

**Verwaltung:** Nur ein vollständig angemeldeter Admin legt Wartungsobjekte an (`/admin/wartung/new`), bearbeitet sie, ändert Intervalle und Vorwarnzeiten oder deaktiviert und reaktiviert sie. Name und ein Startdatum der letzten Wartung sind erforderlich; das Formular schlägt heute vor. Die Kategorie startet als „Allgemein“; Beschreibung, Standort und Wartungshinweise/Checkliste sind optional. Das Intervall besteht aus einem Wert von 1 bis 1000 und der Einheit Tage, Wochen, Monate oder Jahre; die berechnete Fälligkeit muss innerhalb der Jahre 1 bis 9999 liegen. Änderungen werden mit bisherigem/neuem Stand und handelndem Admin auditiert. Versionsprüfungen verhindern das Überschreiben zwischenzeitlicher Änderungen.

**Wartung durchgeführt:** Für ein aktives Objekt können alle aktiven Nutzer eine Durchführung mit Datum, optionaler Notiz und optionalem Befund **In Ordnung** oder **Mangel festgestellt** eintragen. Ohne Auswahl bleibt „Kein Befund angegeben“ erhalten. Der durchführende Nutzer wird bei jedem Eintrag ausdrücklich aus der Namensliste gewählt, auch wenn bereits eine Sitzung besteht. Eine vorherige Anmeldung ist dafür nicht erforderlich. Die Auswahl bestimmt Autor, Signatur und Audit-Akteur, ohne eine Sitzung zu erzeugen oder zu wechseln. Das tatsächliche Durchführungsdatum darf rückwirkend, aber nicht in der Zukunft liegen. Davon getrennt werden Name und Zeitpunkt der Erfassung unveränderlich signiert. Vorgesetzte und Admins können einen vorhandenen Eintrag aus ihrer Sitzung einmalig abzeichnen; auch diese Signatur bleibt erhalten. Eine Abzeichnung der eigenen Durchführung ist für diese Rollen erlaubt.

Für die Fälligkeitslogik und Historie gelten folgende Annahmen:

- Ausgangspunkt ist das **neueste** Datum aus dem beim Anlegen gesetzten Startdatum und allen erfassten Durchführungen. Ein nachträglich erfasster älterer Eintrag ergänzt die Historie, dreht den Termin aber nicht zurück. Das Startdatum bleibt nach der Anlage unveränderlich; weitere Durchführungen werden als neue Historieneinträge erfasst.
- Nächste Fälligkeit ist dieses Datum plus das aktuell eingestellte Intervall. Die Berechnung zählt Kalendertage bzw. Kalenderwochen, keine festen 24-Stunden-Zeiträume. Bei Monaten und Jahren wird ein nicht vorhandener Tag auf das Monatsende begrenzt: 31.01. + ein Monat ergibt 28.02. bzw. 29.02.; 29.02. + ein Jahr ergibt 28.02. Das Intervall beginnt mit der tatsächlichen Durchführung neu.
- Eine erfasste Wartung verändert die Fälligkeit **sofort**. Das spätere Abzeichnen dokumentiert die Prüfung und verzögert die Neuberechnung nicht. Auch ein Befund „Mangel festgestellt“ bleibt als Befund erhalten, erzeugt aber keine automatische Aufgabe oder Werkzeugmeldung.
- Die globale Vorwarnzeit beträgt zunächst **sieben Kalendertage** und wird vom Admin unter `/admin/wartung/settings` in der Datenbank eingestellt, getrennt von `reminder_days` für Aufgaben. Ein leeres Vorwarnfeld am Objekt übernimmt den jeweils aktuellen globalen Wert; ein eigener Wert von 0 bis 365 Tagen überschreibt ihn.
- „Bald fällig“ umfasst heute bis einschließlich des letzten Vorwarntages. Mit Vorwarnzeit 0 erscheint nur das heute fällige Objekt als „Bald fällig“. „Überfällig“ beginnt am folgenden Werkstatttag. Tagesgrenzen verwenden `timezone`, standardmäßig **Europe/Berlin**, einschließlich Sommer-/Winterzeitwechseln. Angezeigte Datumswerte haben das Format **TT.MM.JJJJ**; Erfassungs- und Audit-Zeitpunkte bleiben UTC.
- Wartungseinträge und Abzeichnungen werden weder bearbeitet noch gelöscht. Objekte werden nur deaktiviert; ihre Historie bleibt sichtbar und weiterhin abzeichenbar. Deaktivierte Objekte nehmen keine neuen Durchführungen an und erzeugen keine Dashboard-Warnungen. Bei Reaktivierung wird die Fälligkeit aus den erhaltenen Daten berechnet.

Die Erweiterung hat ein eigenes Paket `internal/wartung` und verwendet die gemeinsamen Nutzer-, Rollen-, Signatur- und Audit-Regeln. Prioritäten und Kommentare werden für diese Wartungshistorie nicht zusätzlich eingeführt. Foto-Anhänge, Benachrichtigungen, Kosten, externe Dienstleister und automatisch erzeugte Azubi-Aufgaben gehören nicht zu dieser Stufe. Alle Funktionen und Assets bleiben lokal; eine Hintergrundplanung oder ein externer Dienst wird für die berechneten Warnungen nicht benötigt.

## Rechte und weitere Annahmen

Lesen sowie die Neuanlage von Ideen, Werkzeugmeldungen, Anschaffungswünschen und Bestandsprüfungen mit Erstellerwahl sind wie oben beschrieben ohne Sitzung möglich. Für weitere Aktionen gelten in einer aktiven Sitzung (bei Admins nach Passwortanmeldung und Pflichtwechsel) folgende Rechte; die ersten Status- und Textregeln beziehen sich auf Ideen:

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
| Verbrauchsmaterialien und Mindestbestände verwalten | Nein | Nein | Ja |
| Bestand mit ausdrücklicher Erstellerwahl prüfen | Ja | Ja | Ja |
| Bestellwarnung signiert abhaken | Nein | Ja | Ja |
| Wartungsobjekte, Intervalle und Vorwarnzeiten verwalten | Nein | Nein | Ja |
| Durchgeführte Wartung mit ausdrücklicher Erstellerwahl erfassen | Ja | Ja | Ja |
| Wartungseinträge abzeichnen | Nein | Ja | Ja |
| Vollständiges Audit-Log ansehen | Nein | Ja | Ja |
| Benutzer anlegen, ändern, deaktivieren, löschen und Rollen setzen | Nein | Nein | Ja, nach Passwortwechsel |

- Mitarbeiter und Vorgesetzte verwenden die einfache Namensauswahl; Admins melden sich mit Passwort an. Eine Erstellerwahl im Beitragsformular stellt keine Anmeldung dar und gewährt keine Admin-Rechte. Eine Signatur ist Name + Serverzeit, keine kryptografische Unterschrift. Die App ist für das vertraute Werkstattnetz vorgesehen.
- Rollenprüfungen erfolgen serverseitig. Inaktive und gelöschte Nutzer erscheinen weder in der Erstellerwahl noch in der Anmeldung. Bestehende Sitzungen berücksichtigen Deaktivierungen, Löschung und Rollenänderungen sofort; öffentliches Lesen bleibt möglich. Admin-Passwortanmeldung und normale Namenssitzungen sind gekapselt. Sitzungen liegen nur im Arbeitsspeicher, laufen ab und enden bei Neustart.
- Generische Basiseinträge wurden aus Oberfläche und HTTP-Routen (`/entries/*`) entfernt. Bereits gespeicherte Basisdaten und deren Audit-Historie werden durch das Update nicht gelöscht. Ideen, Aufgaben, Werkzeugmeldungen und Anschaffungswünsche verwenden die oben beschriebenen eigenen Statusabläufe.
- Signaturen und Autor bleiben unverändert, auch nach einer Benutzerumbenennung. Kommentare sind nach dem Absenden unveränderlich. Signatur- und Audit-Zeiten werden in UTC gespeichert und angezeigt; Aufgabenfälligkeiten verwenden lokale Kalenderdaten.
- Änderungen an Einträgen, Kommentare und Benutzerverwaltung werden atomar mit dem Audit-Ereignis gespeichert: Akteur als ID und damaliger Name, Serverzeit, Aktion und vollständiger vorheriger/neuer Zustand. Bei einem Audit-Fehler wird die gesamte Änderung zurückgerollt. Parallele Eintragsbearbeitungen erkennen veraltete Versionen und überschreiben diese nicht.
- Das Audit-Log ist über App und Datenbank-Trigger nur erweiterbar. Die gemeinsame Anwendungsschicht schützt alle Schreibvorgänge; direkter Zugriff auf die SQLite-Datei gehört zum vertrauenswürdigen Betrieb und ist kein unterstützter Schreibweg.
- Eine aktive Admin-Person muss erhalten bleiben. Der letzte aktive Admin kann weder gelöscht, deaktiviert noch heruntergestuft werden.
- Ideen, Aufgaben, Werkzeuge, Meldungen und Anschaffungswünsche einschließlich Kommentaren, Verbrauchsmaterialien mit Bestandsprüfungen/Warnungshistorie sowie Wartungsobjekte und Wartungshistorien sind ohne Anmeldung im Werkstattnetz sichtbar. Das Kummerkastenmodul folgt noch. Vertrauliche Kummerkasten-Inhalte erst im entsprechend geschützten späteren Modul erfassen.

## Bestehende Installation aktualisieren

Bei einem Update einer bestehenden Installation zuerst die App stoppen und das vollständige Datenverzeichnis sichern. Dann Branch `alpha` aktualisieren, das neue Programm bauen bzw. mit `go run` starten und dieselbe Konfiguration sowie Datenbank verwenden. **`init` nicht erneut ausführen.** Fehlende versionierte Migrationen werden beim Start automatisch angewendet. Die neue Migration **007** ergänzt Verbrauchsmaterialien, Bestandsprüfungen und Warnungen. Die gemeinsamen Eintrags- und Kindtabellen werden transaktional erweitert, damit Bestandsprüfungen dieselben Kommentare und Prioritäten verwenden können. IDs, bisherige Tabellenwerte, Basis- und Fachbeiträge, Aufgabenabnahmen, Kommentare, Prioritäten, Signaturen, Wartungen einschließlich Abzeichnungen, Admin-Zugangsdaten und Audit-Daten bleiben erhalten; bisherige Migrationen werden nicht verändert. Bei älteren Installationen werden auch Migration 006 für Wartungen und Migration 005 für Admin-Zugangsdaten sowie Lösch- und Sitzungsversionen angewendet: Bereits vorhandene aktive Admins ohne Passwort erhalten beim ersten anschließenden Serverstart jeweils ein einmaliges Startpasswort in der lokalen Konsole und müssen es ändern. Bereits gesetzte Admin-Passwörter werden nicht zurückgesetzt. Neue Konfigurationswerte sind für die Verbrauchsmaterialien nicht nötig. Eine bereits aktualisierte Datenbank benötigt mindestens diese Programmversion; für eine Rückkehr zum alten Programm die vorherige Sicherung verwenden.

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
internal/auth/                 Passwortprüfung und Erzeugung von Startpasswörtern
internal/ideas/                Eigenes Ideenmodell und Statusregeln
internal/tasks/                Aufgaben, Statusrechte und Kalender-Erinnerungen
internal/tooling/              Werkzeugbestand, Zustandsmeldungen und Anschaffung
internal/materials/            Verbrauchsmaterialien, genaue Mengen und Warnungslogik
internal/wartung/              Wartungsobjekte, Kalenderfälligkeiten und Rechte
internal/storage/              SQLite, Transaktionen und Audit-Log
internal/storage/migrations/   Versioniertes Schema
internal/web/                  HTTP, Sitzungen, Templates, lokale Assets
```

Tests verwenden ausschließlich temporäre Verzeichnisse. Datenbanken, Uploads, Sicherungen, Logs, lokale Konfiguration und Geheimnisse dürfen nicht versioniert werden; `.gitignore` schützt die üblichen Pfade und Dateiformate. Beispielkonfiguration enthält ausschließlich lokale Standardwerte. Änderungen ausschließlich auf Branch `alpha`, in kleinen thematischen Commits; niemals direkt auf `main` pushen.

Ideenboard, Aufgabenboard, Werkzeugverwaltung, Verbrauchsmaterialien und Werkstattwartung sind eigene Fachpakete und nutzen die gemeinsame Benutzer-, Signatur- und Audit-Basis. Die Werkstattwartung ist der auf Wunsch vorgezogene Zusatzmeilenstein gemäß `ERWEITERUNG_Werkstattwartung.md`; Verbrauchsmaterialien mit Bestandswarnungen und Abhaken erfüllen Meilenstein 5. Als nächster Schritt folgt der Kummerkasten (Meilenstein 6), danach erweiterte Administration/Uploads und Pi-Betrieb gemäß `AGENTS.md`.

Version **0.6.0** wurde mit vollständigen Race-Tests, `go vet` und Builds ohne CGO für AMD64 und ARM64 geprüft. Fachtests decken genaue Dezimalmengen, Mindestbestandsgrenzen, wiederholte Prüfungen, Auffüllung ohne automatische Bestätigung, erneute Rückgänge nach dem Abhaken und konkurrierende Bestätigungen ab. Auch Schwellenänderungen während einer Deaktivierung, Rechte, Versionskonflikte, unveränderliche Prüfwerte, Prioritäten/Kommentare und erhaltene Signaturen nach Nutzerlöschung sind geprüft. Audit-Fehler rollen Anlage, Stammdatenänderung, Bestandsprüfung sowie Auslösen und Abhaken von Warnungen vollständig zurück. Migration 007 und erneutes Öffnen erhalten sämtliche bisherigen Tabellenwerte einschließlich Wartungshistorie, Admin-Zugangsdaten, Aufgabenabnahmen und Audit-Log; Indizes, Fremdschlüssel und Schutzregeln bleiben wirksam.

Der vollständige Chromium-Ablauf bei 320–1440 Pixeln bestätigt Materialverwaltung, Erstellerwahl, Mengen mit Komma und Punkt, Warnungszyklen, Kommentare, beide Prioritäten, Versionskonflikte, historische Signaturen, Deaktivierung/Reaktivierung und Dashboard. Auch maximale Mengen und lange Einheiten wurden geprüft. Die drei bisherigen Dashboard-Spalten bleiben erhalten; es gibt keine horizontalen Überläufe oder externen Anfragen. Noch kein Lauf auf echter Pi-Hardware.
