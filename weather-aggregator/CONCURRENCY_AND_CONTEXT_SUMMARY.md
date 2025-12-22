# Zusammenfassung: Context, HTTP-Handling & Nebenläufigkeit

Datum: 22. Dezember 2025

Dieses Dokument fasst die Änderungen, Gründe und Empfehlungen zusammen, die bei der Review und Refactorings am Projekt vorgenommen wurden.

## Wichtige Änderungen im Code
- `WeatherSource.Fetch(ctx context.Context, city string) WeatherData`
  - `context.Context` wird jetzt durch alle Fetch-Methoden propagiert.
- `fetchWeatherConcurrently(ctx, city, sources)` und `fetchSequential(ctx, ...)`
  - Beide Funktionen nehmen jetzt einen `context.Context` als ersten Parameter.
- HTTP: gemeinsamer Helper `doGet(ctx, url)` verwendet `http.NewRequestWithContext`.
- Globaler `http.Client` bleibt, aber mit vereinfachter Konfiguration:
  - `Timeout: 10 * time.Second` (DefaultTransport genutzt)
- Gemeinsame Geocoding-Funktion `lookupLatLon(ctx, city)` erstellt DRY-Code für mehrere Quellen.
- Robusteres Parsing (`strconv.ParseFloat`) und Fehler-Wrapping (`%w`).

## Warum `context.Context` eingeführt wurde
- `context.Context` erlaubt Abbruch (Cancel) und Deadlines/Timeouts konsistent über Goroutinen und HTTP-Requests.
- Mit `NewRequestWithContext` werden HTTP-Requests beim Abbruch sauber beendet (Sockets geschlossen, Decoder abgebrochen).
- Ohne Context besteht die Gefahr, dass Langläufer Ressourcen belegen oder das Programm lange hängt.

Beispiel: In `main.go` wird ein Gesamt-Timeout gesetzt:

```go
ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
defer cancel()
```

Jeder Fetch erbt dieses Limit; einzelne Requests haben zusätzlich `client.Timeout = 10s`.

## Erklärung des gemeldeten TLS-Handshake-Timeouts
- Fehlermeldung: `net/http: TLS handshake timeout` bedeutet, dass der TLS-Handshake (Teil des Verbindungsaufbaus) vom Server nicht innerhalb des vom Client erlaubten Zeitfensters abgeschlossen wurde.
- Ursachen: Serverüberlastung, Netzprobleme, Rate-Limiting, oder langsame TLS-Konfiguration auf Serverseite.
- In deinem Setup gilt:
  - `client.Timeout = 10s` begrenzt die Gesamtdauer eines einzelnen HTTP-Requests (inkl. TLS-Handshake).
  - `context.WithTimeout(..., 15s)` begrenzt die gesamte Operation (alle parallel laufenden Requests). Bei langsamen TLS-Handshake endet der einzelne Request nach 10s mit dem TLS-Timeout, unabhängig vom 15s-Kontext.

Empfehlung: Bei wiederkehrenden TLS-Timeouts mit einer bestimmten Quelle kann ein erhöhtes `client.Timeout` (z. B. 20s) oder gezieltes Retry-Handling helfen. Für die meisten APIs ist 10s jedoch angemessen.

## Funktionsweise des gepufferten Channels im aktuellen Code
- Channel wird mit `len(sources)` gepuffert: `ch := make(chan WeatherData, len(sources))`.
- Jede Goroutine schreibt ihr Resultat in den Channel: `ch <- src.Fetch(ctx, city)`.
- Buffer erlaubt den Goroutinen, das Ergebnis zu senden, ohne zu blockieren, selbst wenn der Empfänger noch nicht liest.
- Die Haupt-Goroutine liest exakt `len(sources)` Ergebnisse; dadurch entsteht kein Deadlock.

Vorteile:
- Verhindert, dass Goroutinen bei `ch <-` blockieren und potenziell hängen, falls der Empfänger verzögert.
- Einfaches Sammeln von Ergebnissen ohne zusätzliche Synchronisierung.

## WaitGroup: Was ist es und wäre es hier sinnvoll?
- `sync.WaitGroup` synchronisiert Abschluss von Goroutinen; es überträgt keine Daten.
- Muster:

```go
var wg sync.WaitGroup
results := make([]WeatherData, len(sources))
for i, s := range sources {
    wg.Add(1)
    go func(i int, src WeatherSource) {
        defer wg.Done()
        results[i] = src.Fetch(ctx, city)
    }(i, s)
}
wg.Wait()
```

Wann sinnvoll?
- Wenn Reihenfolge/Indexierung wichtig ist (Ergebnisse am gleichen Index speichern).
- Wenn du nur auf Abschluss warten willst und die Ergebnisse anderweitig (z. B. über Shared Slice) sammelst.

Warum in deinem Projekt der gepufferte Channel besser passt:
- Du sammelst Ergebnisse (Channel ist ein natürlicher Fit).
- Reihenfolge ist für die Aggregation egal.
- Channel-Verwendung ist klar und idiomatisch ("share by communicating").

## Empfehlungen kurz gefasst
- Behalte `context.Context` in allen Fetch-Pfaden.
- `client.Timeout = 10s` ist für die meisten APIs ausreichend; erhöhe nur bei konkreten Problemen mit einer Quelle.
- Verwende gepufferten Channel wie aktuell für einfache Result-Collection. Nutze `WaitGroup` nur, wenn du Index-Positionen brauchst oder nur Completion überwachen willst.
- Logge detailliertere Fehler (URL, status) beim Debugging von TLS/Timeout-Problemen.

## Performance-Analyse: Concurrent vs. Sequential mit echten Daten

### Das Paradoxon: Warum Sequential schneller *aussah*
Bei deinem initialen Test war Sequential schneller als Concurrent — **aber nicht aus strukturellen Gründen**, sondern weil **wttr.in timeout-t**:

| Modus | Mit wttr.in | Ohne wttr.in |
|-------|-------------|-------------|
| **Concurrent** | **~10s** (Gesamtdauer bis Context-Deadline) | **~310ms** |
| **Sequential** | ~11-15s (addiert alle Einzelzeiten) | ~716ms |

### Detaillierte Pro-API Timing (Concurrent, ohne wttr.in):
```
✅ Meteosource:    85.6ms   ← schnellste
✅ WeatherAPI.com: 103.5ms
✅ Open-Meteo:     159.3ms
✅ Weatherstack:   167.7ms
✅ Pirate Weather: 310.0ms  ← langsamste
─────────────────────────────────────
🎯 Gesamtdauer:    310ms    ← entspricht der langsamsten (parallele Ausführung!)
```

### Warum Concurrent mit wttr.in langsam war:
1. 6 Goroutinen starten parallel
2. wttr.in TLS-Handshake hängt sich auf
3. Nach 10s: `client.Timeout` triggert → wttr.in bricht ab
4. Main-Goroutine muss trotzdem auf alle 6 Ergebnisse warten
5. Nach 15s: `context.WithTimeout` killiert auch die schnellen Requests → **Gesamtdauer: ~10s**

### Warum Sequential mit wttr.in noch langsamer wäre:
- Open-Meteo: ~160ms
- WeatherAPI.com: ~100ms
- Weatherstack: ~190ms
- Meteosource: ~80ms
- **wttr.in: ~10s (timeout)**
- Pirate Weather: ~310ms
- **Gesamtdauer: ~10.8s** (Addition aller Zeiten)

### Die Lektion: Concurrency ist nicht immer schneller!
Concurrency ist optimal, wenn:
- ✅ Alle Tasks ungefähr gleiche Dauer haben
- ✅ Es eine Mischung aus schnellen und langsamen Tasks gibt
- ⚠️ ABER wenn eine Task sehr langsam ist und es eine gemeinsame Deadline gibt, blockiert sie alle

**Lösung:** `--exclude` Flag verwenden!

## Neues Feature: `--exclude` Flag zur Source-Filterung

Du kannst jetzt problematische Quellen ausschließen:

```bash
# Mit problematischer Quelle wttr.in
./weather-aggregator --city=Berlin
# Ergebnis: ~10s (wttr.in timeout-t)

# Ohne wttr.in — viel schneller!
./weather-aggregator --city=Berlin --exclude=wttr.in
# Ergebnis: ~310ms ⚡

# Mehrere Quellen ausschließen
./weather-aggregator --city=Berlin --exclude=wttr.in,Meteosource
```

### Timing-Ausgabe pro API
Jeder Request zeigt jetzt die Dauer an:
```
✅ Meteosource:       2.2°C, 0% humidity, Overcast [85.6ms]
✅ WeatherAPI.com:    2.0°C, 93% humidity, Mist [103.5ms]
✅ Open-Meteo:        2.7°C, 90% humidity, Rainy [159.3ms]
✅ Weatherstack:      2.0°C, 93% humidity, Mist [167.7ms]
✅ Pirate Weather:    3.7°C, 73% humidity, Overcast [310.0ms]
❌ wttr.in:           ERROR: ... context deadline exceeded [10000.2ms]
```

## Best Practices für Live-Demo/Präsentation

### Demo-Szenarien:
1. **Schneller Case** (alle APIs responsiv):
   ```bash
   ./weather-aggregator --city=Berlin --exclude=wttr.in
   # Zeigt: Concurrent ist viel schneller (310ms vs. 716ms Sequential)
   ```

2. **Langsamster Case** (mit wttr.in):
   ```bash
   ./weather-aggregator --city=Berlin
   # Zeigt: Context-Timeout bei 10s, Gesamtoperation bei 15s
   # Das Problem: Langsamste Quelle blockiert alle
   ```

3. **Sequential-Vergleich** (ohne wttr.in):
   ```bash
   ./weather-aggregator --city=Berlin --sequential --exclude=wttr.in
   # Zeigt: Sequential braucht ~716ms (addiert alle)
   # Concurrent braucht ~310ms (parallele Ausführung)
   ```

### Was die Demo zeigt:
- ✅ Concurrency bei homogenen Tasks: **2-3x schneller**
- ⚠️ Concurrency + sehr langsame Quelle: **blockiert**
- ✅ Lösung: Problematische Quellen ausschließen
- ✅ Context + Timeout: **graceful degradation** statt Hangs

### Diskussionspunkte:
1. **Warum ist Concurrent nicht immer besser?**
   - Shared Deadline (context.WithTimeout)
   - Eine langsame Goroutine kann alle blockieren

2. **Warum brauchen wir Context?**
   - Ohne Context: wttr.in-Request läuft ewig
   - Mit Context: Nach 10s abgebrochen (Client.Timeout) oder 15s (Overall Timeout)

3. **Buffered Channel Nutzen?**
   - Verhindert Goroutine-Leak auch wenn Hauptprogramm early-exit macht
   - Zeigt Unterschied zu WaitGroup (würde auch funktionieren, aber weniger idiomatisch)

## Quick-Commands für Demo
```bash
# Schneller Case (empfohlen)
./weather-aggregator --city=Berlin --exclude=wttr.in

# Langsamer Case (zeigt Timeout-Verhalten)
./weather-aggregator --city=Berlin

# Sequential Vergleich
./weather-aggregator --city=Berlin --sequential --exclude=wttr.in

# Mit Debug: alle Tests bestehen
go test ./... -v
```


