# Changelog

Alle relevanten Änderungen am Werkstatt-Hub werden hier dokumentiert.

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
