# Changelog

Alle relevanten Änderungen am Werkstatt-Hub werden hier dokumentiert.

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
