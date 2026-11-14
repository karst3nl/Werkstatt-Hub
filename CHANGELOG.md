# Changelog

Alle relevanten Änderungen am Werkstatt-Hub werden hier dokumentiert.

## 0.4.2 – 2026-11-14

### Geändert

- Generische Basiseinträge aus Dashboard und HTTP-Routen entfernt; vorhandene Daten und Audit-Historie bleiben erhalten.
- Admin-Konto wird beim ersten Serverstart automatisch eingerichtet. Ein zufälliges Startpasswort wird einmalig lokal ausgegeben und muss vor geschützten Aktionen geändert werden.
- Admin-Anmeldung mit Passwort, gesalzenem Passwort-Hash und Widerruf bisheriger Sitzungen bei Passwort- oder Rollenänderungen.
- Nutzer können ausschließlich von vollständig angemeldeten Admins erstellt, geändert, deaktiviert und gelöscht werden. Historische Nutzerbezüge bleiben bei Löschung erhalten; der letzte aktive Admin ist geschützt.
- Neue Admins, Beförderungen und Passwort-Rücksetzungen verwenden temporäre Passwörter mit Wechselpflicht.
- Prioritätsanzeige mit fünf lokalen SVG-Schraubenschlüsseln, gefüllten Stufen von Grün bis Rot und grauen übrigen Symbolen; ursprüngliche Werte und konfigurierbare Skala bleiben erhalten.
- Migration 005 ergänzt Zugangsdaten sowie Nutzer-Löschstatus und Sitzungsversionen ohne Entfernen historischer Beiträge.

## 0.4.1 – 2026-11-14

### Geändert

- Dashboard zeigt Aufgaben, Werkzeuge/Anschaffungen und Ideen auf breiten Bildschirmen in drei Spalten von links nach rechts.
- Auf schmalen Bildschirmen bleiben die Bereiche in derselben Reihenfolge untereinander lesbar; Basiseinträge folgen unterhalb.
- Die breitere Dashboardansicht und die Anpassung der Karten gelten nur für die Startseite.

## 0.4.0 – 2026-11-14

### Hinzugefügt

- Öffentlicher Werkzeugbestand mit Name, Kategorie, Standort, offiziellem Zustand und Filter-/Suchfunktionen.
- Signierte Zustandsmeldungen mit Bearbeitungsstatus, Kommentaren und beiden Prioritäten; offene Meldungen werden am Werkzeug und im Dashboard markiert.
- Anschaffungswünsche mit Erstellerwahl, optionalem Werkzeugbezug und Zielstandort sowie Status „Gewünscht“, „Genehmigt“, „Bestellt“ und „Geliefert“.
- Rollenabhängige Pflege des Werkzeugbestands und Beschaffungsstatus; Mitarbeiter bearbeiten eigene Wünsche nur im Status „Gewünscht“.
- Werkzeugbezogene Historie für Bestandsänderungen, verknüpfte Meldungen, Wünsche und Kommentare, getrennt vom geschützten vollständigen Audit-Log.
- Gemeinsame Signaturen, Prioritätsfilter und atomare Audit-/Versionsprüfung auch für Werkzeugmeldungen und Anschaffungswünsche.
- Migration 004 zur Erweiterung des Schemas unter Erhalt bisheriger Daten einschließlich Aufgabenabnahmen.

### Annahmen

- Zustandsmeldungen ändern den offiziellen Werkzeugzustand nicht automatisch; Vorgesetzte oder Admins prüfen die Meldung und pflegen den Bestand ausdrücklich.
- Ein Werkzeugbezug bleibt nach der Anlage unveränderlich. „Geliefert“ erzeugt keinen Inventareintrag; neue Werkzeuge werden separat erfasst.
- Öffentliche Meldungs- und Wunscherstellung verwendet die Erstellerwahl ohne Sitzungswechsel. Das Anlegen eines Werkzeugs erfordert eine berechtigte Sitzung; Signatur und handelnder Audit-Akteur können sich unterscheiden.

## 0.3.0 – 2026-11-14

### Hinzugefügt

- Aufgabenboard als eigenes Modul mit Kanban-Status, Zuweisung und verpflichtendem Fälligkeitsdatum.
- Öffentliche Aufgabenübersicht und Details sowie persönliche Ansicht nach Namensauswahl.
- Erinnerungen auf Dashboard und in der eigenen Aufgabenansicht für überfällige, heute und bald fällige Aufgaben.
- Konfigurierbare Werkstatt-Zeitzone (`Europe/Berlin`) und Erinnerungszeitraum (drei Kalendertage); Zeitzonendaten werden lokal mitgeliefert.
- Rollenabhängige Statusänderungen und signierte Abnahme durch Vorgesetzte/Admins; Wiederöffnen erhält frühere Abnahmen im Audit.
- Gemeinsame Kommentare, beide Prioritäten und atomarer Audit-Verlauf auch für Aufgaben, mit Versionsprüfung gegen konkurrierende Änderungen.
- Versionierte Migration 003 unter Erhalt vorhandener Einträge, Ideen, Kommentare, Signaturen und Audit-Historie.

### Annahmen

- Alle aktiven Mitarbeiter können Aufgaben erhalten, solange es keine eigene Azubi-Kennzeichnung gibt; Ausbilder verwenden die Vorgesetztenrolle.
- Das Anlegen einer Aufgabe erfordert eine berechtigte Namenssitzung. Der gewählte berechtigte Ersteller bestimmt die Signatur, der handelnde Sitzungsnutzer bleibt Audit-Akteur.
- „Bald fällig“ umfasst standardmäßig die nächsten drei Kalendertage einschließlich des letzten Tages. Fertige Aufgaben erzeugen keine Erinnerungen.

## 0.2.1 – 2026-11-14

### Geändert

- Website öffnet direkt das Dashboard; Basiseinträge, Ideen und Kommentare sind ohne Anmeldung lesbar.
- Jedes neue Beitragsformular fragt nach einem aktiven Ersteller. Die Auswahl bestimmt die unveränderliche Signatur und den Audit-Akteur, ohne eine Sitzung zu erzeugen oder zu wechseln.
- Namenssitzung bleibt für Kommentare, Bearbeitungen und Prioritäten sowie rollenabhängige Verwaltungsaktionen bestehen.
- Abmeldung führt zum Dashboard; abgelaufene oder deaktivierte Sitzungen blockieren öffentliche Ansichten nicht.
- Tests für öffentlichen Zugriff, ungültige Ersteller, Signaturen/Audit, Sitzungsgrenzen und spätere private Module ergänzt. Keine neue Datenbankmigration notwendig.

## 0.2.0 – 2026-11-14

### Hinzugefügt

- Ideenboard als eigenes Fachmodul mit Kategorien, signierten Kommentaren und Status bis „umgesetzt“.
- Zwei unabhängig gepflegte Prioritäten für Ideen und Basiseinträge, einschließlich Löschen, Rollenprüfung und atomarem Audit-Verlauf.
- Gemeinsame konfigurierbare Prioritätsskala (Standard 1–5); unzulässige Skalen und Ausschluss bestehender Werte werden abgewiesen.
- Kombinierbare Kategorie-, Status- und Prioritätsfilter, Sortierung nach beiden Prioritäten und sichtbare Kennzeichnung starker Abweichungen.
- Ideenübersicht auf der Startseite sowie lokale, responsive Formulare für Board und Prioritäten.
- Versionierte Migration 002 unter Erhalt vorhandener IDs, Signaturen, Kommentare und Audit-Einträge.
- Tests für neue Rechte, Prioritäten, Audit-Rollback, Konflikte, Filter/Sortierung und das Update von Meilenstein 1.

### Annahmen

- Kategorien sind zunächst frei benannte Pflichtfelder; zentrale Verwaltung folgt mit Meilenstein 7.
- Mitarbeiter-Priorität ist ein gemeinsamer Wert für Mitarbeiter und den jeweiligen Ersteller, keine Abstimmung. Vorgesetzte und Admins bearbeiten die getrennte Vorgesetzten-Priorität.

## 0.1.0 – 2026-11-14

### Hinzugefügt

- Projektgerüst mit Go, SQLite, lokaler JSON-Konfiguration und eingebetteten Assets.
- Namensauswahl mit separater Admin-Anmeldung und gekapselten Sitzungen.
- Servergeprüfte Rollen Mitarbeiter, Vorgesetzter und Admin; grundlegende Benutzerverwaltung und einmalige Admin-Einrichtung.
- Gemeinsame Basiseinträge und Kommentare mit unveränderlicher Signatur aus Name und Serverzeit.
- Transaktionales, gegen Überschreiben und Löschen geschütztes Audit-Log mit vorherigem/neuem Stand.
- Versionierte SQLite-Migrationen mit Prüfsummen und Erkennung paralleler Eintragsänderungen.
- Tests für Rechte, Audit-Log, Migrationen und HTTP-Sicherungen; Anleitung für lokalen Start und Pi-Build.

### Noch ausstehend

- Fachmodule und vollständiges Prioritätssystem gemäß Meilensteinen 2–7.
- systemd-Service, automatische Backups und Pi-Feinschliff gemäß Meilenstein 8.
