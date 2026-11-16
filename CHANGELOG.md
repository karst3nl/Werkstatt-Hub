# Changelog

Alle relevanten Änderungen am Werkstatt-Hub werden hier dokumentiert.

## 0.6.0 – 2026-11-16

### Hinzugefügt

- Verbrauchsmaterialien mit Name, Einheit, Lagerort, aktuellem Bestand und Mindestbestand sowie administrativer Anlage, Bearbeitung und Deaktivierung/Reaktivierung.
- Öffentliche Übersicht mit Suche und Filtern für niedrigen Bestand sowie aktive/deaktivierte Materialien; Detailansichten mit Prüf- und Warnungshistorie.
- Signierte Bestandsprüfungen mit ausdrücklicher Erstellerwahl, genauer Speicherung in Tausendsteln und Versionsprüfung gegen zwischenzeitliche Änderungen.
- Unveränderliche Prüfwerte mit eigener Beitragsansicht, beiden Prioritäten und Kommentaren nach den gemeinsamen Rechte- und Audit-Regeln.
- Bestellwarnungen auf oder unter dem Mindestbestand, signiertes Abhaken ausschließlich durch Vorgesetzte/Admins und erneute Warnung bei späterem Bestandsrückgang.
- Verbrauchsmaterial-Warnblock oberhalb der Wartung und bisherigen Dashboard-Spalten; offene Warnungen bleiben bis zum ausdrücklichen Abhaken sichtbar.
- Migration 007 für Material-, Prüf- und Warnungsdaten sowie Erweiterung der gemeinsamen Eintragstabellen unter Erhalt bisheriger Daten und Schutzregeln.

### Annahmen

- Stammdaten sind Admins vorbehalten. Eine Materialeinheit bleibt nach der Anlage unveränderlich; weitere Bestandsänderungen erfolgen ausschließlich durch neue Prüfungen.
- Alle aktiven Rollen dürfen den aktuellen Bestand mit ausdrücklich gewähltem Ersteller ohne vorherige Sitzung erfassen. Die Auswahl erzeugt keine Sitzung oder Verwaltungsrechte.
- Mengen sind nichtnegativ, auf drei Nachkommastellen begrenzt und ohne Fließkommarundung gespeichert. Komma und Punkt werden als Dezimaltrenner akzeptiert.
- Es gibt höchstens eine offene Warnung pro Material. Eine Auffüllung allein hakt sie nicht ab. Nach dem Abhaken erzeugt derselbe oder ein gestiegener niedriger Bestand keine neue Warnung; ein erneuter Rückgang auf oder unter das Minimum beginnt einen neuen Zyklus.
- Deaktivierte Materialien behalten Historie und offene Warnungen, erhalten keine neuen Prüfungen und erzeugen keine Dashboard-Warnung. Eine Reaktivierung zeigt erhaltene offene Warnungen wieder an.
- Prüfzeitpunkte kommen vom Server. Eine Korrektur erfolgt als neue Prüfung; wiederkehrende Azubi-Aufgaben und Prüfintervalle bleiben späteren Erweiterungen vorbehalten.

## 0.5.0 – 2026-11-14

### Hinzugefügt

- Eigenes Modul und öffentlicher Reiter „Werkstattwartung“ mit nach Fälligkeit sortierten Objekten, Kategorie-/Status-/Aktivitätsfiltern und Detailansichten.
- Admin-Verwaltung für Wartungsobjekte, Intervalle in Tagen/Wochen/Monaten/Jahren, individuelle Vorwarnzeiten, Deaktivierung/Reaktivierung und globale Standardvorwarnzeit.
- Zentrale Kalenderberechnung mit Begrenzung auf Monatsenden, Schaltjahrbehandlung und Tagesgrenzen in der Werkstatt-Zeitzone.
- Rückwirkend erfassbare, signierte Wartungsdurchführungen mit Notiz und Befund sowie einmalige Abzeichnung durch Vorgesetzte/Admins; Durchführungshistorie und Abzeichnungen sind unveränderlich.
- Wartungsblock mit Anzahlen bald fälliger und überfälliger Objekte sowie hervorgehobenen überfälligen Wartungen oberhalb der Dashboard-Spalten.
- Versionierte Migration 006 mit zusätzlichen Wartungstabellen; bestehende Daten und Migrationen bleiben unverändert.
- Rechte- und Audit-Regeln für alle Wartungsschreibvorgänge sowie Versionsprüfung für administrative Änderungen.

### Annahmen

- Die Werkstattwartung wird als eigener Zusatzmeilenstein vorgezogen. Verbrauchsmaterialien (Meilenstein 5) bleiben offen.
- Alle aktiven Nutzer dürfen durchgeführte Wartungen mit ausdrücklicher Erstellerwahl ohne vorherige Sitzung erfassen; die Auswahl erzeugt keine Sitzung oder Verwaltungsrechte.
- Das neueste Datum aus Startwert und erfassten Durchführungen bestimmt die nächste Fälligkeit. Ältere Nachträge drehen den Termin nicht zurück; zukünftige Durchführungsdaten werden abgewiesen.
- Ein neuer Durchführungseintrag berechnet die Fälligkeit sofort neu. Die spätere Abzeichnung dokumentiert die Prüfung und ist keine Voraussetzung dafür.
- Globale Vorwarnzeit zunächst sieben Kalendertage; ein leeres Objektfeld übernimmt den aktuellen globalen Wert. Vorwarnzeit 0 erfasst nur den Fälligkeitstag; überfällig wird ein Objekt am Folgetag.
- Startdatum nach der Objektanlage unveränderlich. Objekte werden deaktiviert und nicht gelöscht; inaktive Objekte behalten ihre Historie, erhalten keine neuen Durchführungen und erzeugen keine Dashboard-Warnungen.
- Keine zusätzlichen Prioritäten/Kommentare, Foto-Anhänge, externen Dienste oder automatischen Azubi-Aufgaben in dieser Stufe.

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
