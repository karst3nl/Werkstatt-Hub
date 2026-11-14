# Erweiterung: Werkstattwartung

Zusatzmodul für den Werkstatt-Hub. Es gelten alle Regeln aus `AGENTS.md` (Branch `alpha`, keine Daten im Repo, alles lokal auf dem Pi, Signatur mit Name und Zeitpunkt, Audit-Log, versionierte Migrationen, Tests).

## 1. Ziel
Ein eigener Reiter **"Werkstattwartung"**. Ein Admin legt Dinge an, die regelmäßig gewartet werden müssen (z. B. Hebebühne, Kompressor, Absauganlage, Reifenmontiermaschine, Feuerlöscher, Werkstattbeleuchtung). Für jedes Ding stellt der Admin ein **Wartungsintervall** ein. Die App zeigt, was wann fällig ist, und dokumentiert jede durchgeführte Wartung.

## 2. Rollen und Rechte
- **Admin:** Wartungsobjekte anlegen, bearbeiten, deaktivieren, Intervalle festlegen und ändern.
- **Vorgesetzter:** alles ansehen, Wartungen abzeichnen oder abnehmen (Annahme).
- **Mitarbeiter und Azubis:** Reiter ansehen, fällige Wartungen sehen, durchgeführte Wartungen eintragen (Annahme: jeder darf eintragen, signiert).
- Jede Aktion wird mit Name und Zeitpunkt signiert und im Audit-Log protokolliert.

## 3. Datenmodell (Vorschlag, Codex darf anpassen)
**Wartungsobjekt**
- Name, Beschreibung, Kategorie, Standort in der Werkstatt
- Wartungsintervall: Wert und Einheit (Tage, Wochen, Monate, Jahre)
- Datum der letzten Wartung (Startwert beim Anlegen frei setzbar)
- Nächste Fälligkeit (wird berechnet)
- Vorwarnzeit: wie viele Tage vorher es als "bald fällig" erscheint (pro Objekt einstellbar, Standardwert global)
- Aktiv oder deaktiviert
- Optional: Wartungshinweis oder Checkliste (Freitext oder einfache Punkte), Hinweis auf Herstellervorgaben

**Wartungseintrag (Historie)**
- Bezug zum Objekt, Datum der Durchführung, durchgeführt von (Signatur: Name und Zeitpunkt)
- Notiz, optional Befund (in Ordnung, Mangel festgestellt)
- Optional später: Anhang (nutzt die Foto-Grundlagen aus dem Adminbereich)

## 4. Logik
- Nächste Fälligkeit = Datum der letzten Wartung + Intervall.
- Wird eine Wartung eingetragen, setzt sich das Datum der letzten Wartung neu und die nächste Fälligkeit wird neu berechnet. Das Intervall zählt ab dem tatsächlichen Durchführungsdatum.
- Ändert der Admin das Intervall, wird die Fälligkeit neu berechnet. Die Änderung wird im Audit-Log festgehalten (alter und neuer Wert).
- Status pro Objekt: **in Ordnung**, **bald fällig** (innerhalb der Vorwarnzeit), **überfällig**.
- Das Eintragen einer Wartung rückwirkend (Datum in der Vergangenheit) muss möglich sein. Datum in der Zukunft nicht.
- Historie bleibt vollständig erhalten, nichts wird überschrieben oder gelöscht. Objekte werden nur deaktiviert.

## 5. Oberfläche
**Reiter "Werkstattwartung"**
- Übersichtsliste aller Objekte, sortiert nach Fälligkeit, mit Farbstatus (grün, gelb, rot) und Text (nicht nur Farbe).
- Filter: Kategorie, Status (bald fällig, überfällig), aktiv oder deaktiviert.
- Detailansicht pro Objekt mit Intervall, nächster Fälligkeit, Hinweisen und Wartungshistorie.
- Button "Wartung durchgeführt" mit Datum (Standard: heute), Notiz und Befund.

**Adminansicht**
- Objekte anlegen, bearbeiten, deaktivieren.
- Intervall und Vorwarnzeit einstellen.
- Globale Standardwerte (z. B. Vorwarnzeit) einstellbar.

**Startseite (Dashboard)**
- Block "Wartung": Anzahl überfälliger und bald fälliger Objekte mit Link in den Reiter.
- Überfällige Wartungen erscheinen sichtbar oben auf der Startseite, ähnlich wie die Verbrauchsmaterial-Warnungen.
- Keine Benachrichtigungen per Mail oder Push (wie im Gesamtprojekt).

## 6. Zusammenspiel mit anderen Modulen (optional, nicht zwingend in dieser Stufe)
- Bei Befund "Mangel festgestellt" kann ein Eintrag im Ideenboard oder in der Werkzeugverwaltung vorgeschlagen werden.
- Wiederkehrende Wartungen sollen später optional als Aufgabe im Azubi-Board erscheinen können. Das Datenmodell soll das nicht verbauen.

## 7. Technische Hinweise
- Neues Modul `wartung` mit eigenem Datenmodell, nutzt Nutzer-, Rollen-, Signatur- und Audit-Logik der gemeinsamen Basis.
- Datumsberechnung zentral und gut getestet (Monatsenden, Schaltjahre, Zeitzone Europe/Berlin).
- Neue Migration für die Tabellen, keine Änderung an bestehenden Daten.
- Tests: Fälligkeitsberechnung, Statuswechsel (in Ordnung, bald fällig, überfällig), Intervalländerung, Rechteprüfung, Historie bleibt erhalten.
- Alle Texte auf Deutsch, Datum im Format TT.MM.JJJJ.

## 8. Reihenfolge
Umsetzen als eigener Meilenstein nach den bestehenden Modulen (zwischen Verbrauchsmaterialien und Kummerkasten oder danach, je nach Stand). Zuerst Datenmodell und Fälligkeitslogik mit Tests, dann Admin-Verwaltung, dann Übersicht, Historie und Dashboard-Block.

## 9. Noch nicht in dieser Stufe
Benachrichtigungen, Foto-Anhänge in der Oberfläche, Wartungsverträge oder Kosten, Zuweisung an externe Dienstleister, automatische Aufgaben im Azubi-Board.
