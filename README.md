# ccu-addon-battery

[:uk: English README](README.en.md)

Batteriewechsel erkennen und Batterien auswerten, für Homematic-Geräte: ein Addon für
[openccu-lite](https://github.com/hobbyquaker/openccu-lite) (und später für die klassische CCU3
und OpenCCU). Es zeichnet die Batteriespannung und die Batterie-leer-Meldung jedes Geräts auf,
erkennt daraus Batteriewechsel, lässt Sie eintragen, welche Batterien eingelegt wurden, und macht
aus dem Protokoll: wie lange Batterien halten, welche Geräte Batteriefresser sind, welche
Batterien am besten abschneiden, was bald fällig ist und was zu kaufen ist.

**Stand: in Entwicklung, noch kein Release.** Was heute funktioniert: die Aufzeichnung; die
Batterietabelle (welches Gerät welche Batterien nimmt); die Wechselerkennung mit ihrem Eingang;
von Hand gebuchte Wechsel mit den eingelegten Batterien (Hersteller, Typ, Preis); die
Gerätebilder, die Haltedauern je Gerät, die Tabs Batterien und Statistik; die API zum Import
vergangener Wechsel und Spannungsverläufe; die Wahl des Datenbank-Speicherorts. Die Prognosen, die
Einkaufsliste und die Benachrichtigungen. Die klassische CCU (CCU3, OpenCCU) wird unterstützt.

## Die Seiten

Die Anwendung öffnet sich unter `/addons/battery/` (auf openccu-lite in der Oberfläche des Systems,
mit dessen Design und Sprache):

- **Geräte:** jedes Batteriegerät mit Bild, Status, Batterien, Spannung, der bisherigen Haltedauer
  seiner Batterien (Durchschnitt, kürzeste, längste, Anzahl – sortierbar, kürzeste zuerst, um die
  Batteriefresser zu finden) und wie lange die aktuellen schon drin sind. Ein Gerät öffnet seine
  Detailseite: der Spannungsverlauf über die gesamte Aufzeichnung mit den Wechseln als Marken und
  den Batterie-leer-Phasen als Bändern, und seine Wechsel, jeder bearbeitbar. *Gewechselt …* bucht
  einen Wechsel.
- **Eingang:** was die Erkennung gefunden hat und auf Ihre Antwort wartet – ja (mit den eingelegten
  Batterien), an einem anderen Tag, oder nein. Darunter alle gebuchten Wechsel.
- **Batterien:** jede je eingetragene Batterie (Hersteller, Typ, Größe): wie viele eingelegt
  wurden, in welchen Geräten und Modellen, wie lange sie hielten, was sie pro Batterie und pro
  Jahr kosteten – und, weil die Haltedauer vom Gerät abhängt, die Haltedauer je Gerätemodell
  hinter jeder Zeile, dazu ein Vergleich innerhalb eines Modells oder einer Größe.
- **Einkauf:** welche Geräte jetzt leer melden oder in einem Zeitraum Ihrer Wahl fällig werden, was
  sie je Batteriegröße brauchen, gegen den Vorrat zu Hause gerechnet, und was zu kaufen ist – als
  Liste kopierbar. Die Prognose dahinter steht in der Spalte *Fällig* der Geräteliste: die erwartete
  Haltedauer ist der Median der eigenen Haltedauern des Geräts, bei weniger als zwei der Median
  seines Modells hier, sonst die Angabe der Batterietabelle; fällig ist der letzte Wechsel plus
  diese Dauer minus zwei Wochen, vorgezogen, wenn der Spannungstrend der letzten Wochen die
  Leer-Schwelle früher erreicht.
- **Statistik:** die letzten zwölf Monate in Zahlen, die Geräte mit den kürzesten Haltedauern, die
  Haltedauer je Modell und je Größe, und die Exporte.

Die **Einstellungsseite** ist davon getrennt (auf openccu-lite unter *Addons → Batterie →
Einstellungen*, auf einer CCU der *Einstellungen*-Knopf des Addons): der Speicherort der Datenbank,
die Benachrichtigungen und, auf einer CCU, der Dienst, sein Protokoll und die Version.
Dienststeuerung und Protokoll sind auf openccu-lite die Seiten des Systems selbst.

**Benachrichtigungen:** über [ntfy](https://ntfy.sh) oder einen eigenen Webhook meldet das Addon,
wenn ein Gerät eine leere Batterie meldet, ein Wechsel fällig oder bald fällig ist oder ein
erkannter Wechsel im Eingang wartet – jede Meldung einmal und frühestens nach einer Woche erneut.
Das System selbst bietet einem Addon noch keinen Kanal; sobald es einen hat, wird er hier der
dritte.

## Was aufgezeichnet wird

| Signal | Wo | Geräte |
| --- | --- | --- |
| `OPERATING_VOLTAGE`, `OPERATING_VOLTAGE_STATUS`, `LOW_BAT`, `BOOTED`, `UNREACH` | Kanal 0 | jedes Homematic-IP-Gerät |
| `LOWBAT`, `UNREACH` | Kanal 0 | jedes BidCos-Gerät |
| `BATTERY_STATE` | der Thermostatkanal | HM-CC-RT-DN, HM-TC-IT-WM-W-EU |

Ein Gerät wird aufgezeichnet, wenn seine Beschreibungen einen der Batterie-Datenpunkte tragen.
Beim Start übernimmt das Addon die Werte aus dem Zustandsspeicher des Systems und, für Homematic
IP, aus dem Cache des Funkprozesses (kein Funkverkehr); BidCos-Werte werden nie gelesen, nur
empfangen. Die Werte werden ausgedünnt: eine Zeile, wenn sich die Spannung um 0,01 V bewegt hat,
wenn sich die Batterie-leer-Meldung geändert hat, oder einmal am Tag. Die Übergänge von
Batterie-leer und Nicht-erreichbar sowie `BOOTED` bleiben als Beleg erhalten.

## Welche Batterien ein Gerät nimmt

[data/batteries.json](data/README.md) ist eine Tabelle von 459 Homematic-Modellen: die
Stromversorgung (Batterie, Akku, Netz, Bus, …), die Batteriefächer (Anzahl und Typ), der
Spannungs-Datenpunkt, die Batterie-leer-Schwelle des Geräts und die Quellen jeder Zeile – für die
meisten Batteriemodelle die Produktseiten und Anleitungen von eQ-3 selbst. Die Daten sind CC0:
nehmen Sie sie für jedes Projekt. Das Addon bettet sie ein, sucht Modelle ohne Rücksicht auf
Groß- und Kleinschreibung (und über ihre Aliasse) und lässt sie je Modell oder je Gerät
korrigieren (eine Fernbedienung im Tischaufsteller, Lithium- oder Akkuzellen). Korrekturen an der
Tabelle sind als Pull Requests willkommen; `python3 data/check.py` prüft sie (die CI führt es aus),
und [tools/](tools/README.md) enthält die Skripte, mit denen sie gebaut wurde.

## Wie ein Batteriewechsel erkannt wird

Ein Neustart des Geräts (`BOOTED`), ein Spannungssprung auf das Frischniveau oder das Erlöschen
der Batterie-leer-Meldung ergibt einen *Kandidaten*. Dieselben halb verbrauchten Zellen neu
einzusetzen sieht eine Weile genauso aus – ihre Spannung erholt sich, sobald die Last weg ist –,
deshalb kommt ein Kandidat erst in den **Eingang**, wenn die Spannung einen Tag später noch das
Frischniveau hält; sonst wird er als Neustart protokolliert. Nichts wird von allein gebucht: Sie
bestätigen eine Erkennung, verwerfen sie oder sagen, dass es ein anderer Tag war. Geräte, die nur
eine Batterie-leer-Meldung liefern, werden erkannt, wenn die Meldung erlischt; für diese und jedes
andere Gerät bucht *Gewechselt …* einen Wechsel von Hand. [docs/detection.md](docs/detection.md)
beschreibt die Regeln.

## Die Batterien und was sie kosten

Bei jedem Wechsel können Sie eintragen, was eingelegt wurde, je Fach: den Hersteller („Varta“), den
Typ („Power on Demand“), ob es Akkus sind, und den Preis einer Batterie. Hersteller und Typ werden
aus dem bisher Eingetragenen vorgeschlagen, damit ein Hersteller nicht auf drei Arten geschrieben
wird; der Preis wird mit dem zuletzt für diesen Hersteller und Typ gezahlten vorbelegt. Eine
*Haltedauer* ist die Zeit zwischen zwei bestätigten Wechseln eines Geräts; die Statistik entsteht
aus den Haltedauern, die laufende zählt in keinem Durchschnitt mit. Die Gerätebilder sind die der
Plattform selbst (der Katalog der WebUI, den auch openccu-lite mitbringt).

## Die API

Alles, was die Seite tut, läuft über die API des Addons unter `/addons/battery/api/v1/`, hinter der
Anmeldung des Systems: Geräte mit ihren Batterieprofilen, Bildern und Haltedauern, die Wechsel
(auflisten, buchen, bestätigen, bearbeiten, löschen), Massenimport vergangener Wechsel (JSON oder
CSV, mit Probelauf, dedupliziert und mit dem Erkannten zusammengeführt), Spannungsverläufe, ein
Erkennungslauf über importierte Verläufe, Wertvorschläge, der letzte Preis, die Statistik und die
Exporte. [docs/api.md](docs/api.md) erklärt sie mit einem curl-Rezept;
[docs/openapi.json](docs/openapi.json) spezifiziert sie.

## Auf openccu-lite

Installieren Sie das Paket über die Addons-Seite (oder, sobald es gelistet ist, aus dem Katalog).
Das Addon läuft eingeschränkt als eigener Benutzer, liest die APIs des Systems mit seinem eigenen
Token (`rpc:read`, `meta:read`) und liefert seine Seite unter `/addons/battery/`, eingebettet in die
Oberfläche des Systems mit dessen Design und Sprache; seine Einstellungsseite ist
`/addons/battery-config/`. Aktualisierungen kommen über die Addons-Seite des Systems.

### Wo die Datenbank liegt

Die Datenbank ist eine SQLite-Datei, `battery.db`. Standardmäßig liegt sie im eigenen Verzeichnis
des Addons auf der Benutzerpartition, `/usr/local/addons/battery/var/`, das jedes Backup des
Systems enthält. Sie kann auf einen vom System eingebundenen USB-Stick verschoben werden (die
Einstellungsseite); der Stick ist **nicht** Teil der Backups des Systems, und die Seite sagt das.
Netzwerkfreigaben werden nicht angeboten: SQLite braucht lokale Dateisperren, die NFS und SMB
nicht geben.

Das Verschieben ist sicher: Die Schreiber halten an (neue Werte warten im Arbeitsspeicher), die
ausstehenden Zeilen werden geschrieben, mit `VACUUM INTO` entsteht eine konsistente Kopie, die
Kopie wird geprüft (`integrity_check`, die Zeilenzahl jeder Tabelle), die Einstellung wird
umgeschaltet, das Addon öffnet die neue Datei, und die alte Datei wird nach Ihrer Wahl behalten
oder entfernt. Schlägt ein Schritt fehl, ändert sich nichts. Wird ein Stick mit der Datenbank
abgezogen, sammelt das Addon die neuen Werte im Arbeitsspeicher (bis zu 20.000 Zeilen), sagt das
auf seiner Seite und im Journal und macht weiter, sobald der Stick zurück ist – es beginnt nie von
sich aus anderswo eine neue Datenbank.

Die Einstellung ist `storage` in `/usr/local/addons/battery/etc/battery.json`
(`{"storage": {"location": "usb:<label>", "folder": "battery"}}`); die Einzelheiten stehen in
[docs/storage.md](docs/storage.md).

### Deinstallation

Entfernt das Programm und behält `/usr/local/addons/battery/etc` (die Einstellungen) und `var`
(die Datenbank) für eine Neuinstallation. Entfernen Sie dieses Verzeichnis von Hand, um die Daten
loszuwerden.

## Auf einer CCU3 oder OpenCCU

Dasselbe Paket lässt sich als klassisches Addon installieren (*Einstellungen → Systemsteuerung →
Zusatzsoftware*, oder `/bin/install_addon` auf OpenCCU). Dort zeichnet der Dienst direkt von den
Schnittstellenprozessen auf – er meldet sich per XML-RPC auf dem Loopback (Port 8292) für ihre
Ereignisse an, liest die Geräteliste und alle Viertelstunde den Kanal 0 der Homematic-IP-Geräte
aus dem Cache des Prozesses – und nimmt Namen und Räume aus der ReGaHSS; der *Einstellungen*-Knopf
des Addons in der WebUI öffnet die Einstellungsseite (Dienst, Protokoll, Datenbank,
Benachrichtigungen, Version), deren Knopf *Batterie öffnen* die Anwendung mit der Sitzung der WebUI
öffnet. Nach außen spricht nur der Webserver. Einstellungen und Datenbank liegen in
`/usr/local/addons/battery/etc` und `var`, in jedem Backup; *Deinstallieren* behält sie, wie oben.
Getestet auf OpenCCU 3.89 (x86_64; die Pakete für `armv7l` und `aarch64` entstehen genauso); eine
CCU3 mit der Firmware von eQ-3 steht noch aus.

## Entwicklung

```sh
go test -race ./...                      # Speicher, Aufzeichnung, Erkennung, Wechsel, Statistik, API, Paketdateien
python3 data/check.py                    # die Batterietabelle
(cd ui && npm ci && npm run check && npm test && npm run build)   # die Anwendung und die Einstellungsseite
VERSION=0.1.0 scripts/build.sh           # dist/battery-<arch>-<version>.tar.gz für aarch64, x86_64, armv7l
```

Die Go-Seite nutzt `addonkit` (Sitzung, CSRF, Token, Manifest) und `litesource` (der Ereignisstrom
mit Wiederaufnahme, der Zustandsspeicher, die Metadaten-API) aus
[ccu-addon-kit](https://github.com/hobbyquaker/ccu-addon-kit); die Anwendung ist Svelte 5 auf
[occulite-ui](https://github.com/hobbyquaker/occulite-ui) mit [uPlot](https://github.com/leeoniya/uPlot)
für den Spannungsverlauf; die Einstellungsseite ist der Rahmen des Kits mit dem eigenen Tab des
Addons, von `ccu-addon-kit vendor` aus [addon/kit.json](addon/kit.json) ins Paket übernommen. Der
Dienst nimmt `-no-session-check` und `-token-file` für die Entwicklung gegen ein anderes System als
das, auf dem er läuft:

```sh
go run ./cmd/battery -addon-dir /tmp/battery -www ui/dist -system http://<system> -token-file ./token -no-session-check
```

## Lizenz

MIT; die Batterietabelle (`data/batteries.json` und ihr Schema) ist CC0-1.0.
