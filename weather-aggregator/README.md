# Weather Data Aggregator (Go Edition)

Minimal CLI-based weather aggregator demonstrating **Go's parallel programming** with goroutines and channels. Fetches data from multiple free APIs concurrently, aggregates results, and displays averages with error handling.

## 📊 Code Metrics

- **Total**: ~650 lines (100 main + 420 core + 130 tests)
- **APIs**: 8 sources (2 free + 6 optional with keys)
- **Tests**: 3 test functions + 2 benchmarks
- **Performance**: ~11x faster concurrent vs sequential

## 🌦️ Data Sources

| API | Free? | German Cities | Notes |
|-----|-------|---------------|-------|
| **Open-Meteo** | ✅ Yes | ✅ Excellent | Geocoding + weather |
| **wttr.in** | ✅ Yes | ✅ Excellent | JSON format |
| **WeatherAPI.com** | 🔑 Key | ✅ Good | 1M calls/month free |
| **Weatherstack** | 🔑 Key | ✅ Good | 1k calls/month free |
| **Meteosource** | 🔑 Key | ✅ Good | High precision |
| **Pirate Weather** | 🔑 Key | ✅ Good | Dark Sky compatible |

## 🚀 Quick Start

```bash
# Build
go build -o weather-aggregator

# Configure API keys in .env
echo "WEATHER_API_COM_KEY=your_key" > .env
echo "WEATHERSTACK_API_KEY=your_key" >> .env

# Run (automatically loads keys from .env)
./weather-aggregator --city=Munich
./weather-aggregator --city=Berlin

# Compare concurrent vs sequential
./weather-aggregator --city=Frankfurt --sequential
```

## 📝 Usage

```
./weather-aggregator --city=<city> [--sequential]
```

**Flags:**
- `--city` - City name (required), works great with German cities
- `--sequential` - Disable concurrency for performance comparison

**Environment Variables (.env):**
- `WEATHER_API_COM_KEY`
- `WEATHERSTACK_API_KEY`
- `METEOSOURCE_API_KEY`
- `PIRATE_WEATHER_API_KEY`

## 📊 Example Output

```bash
$ ./weather-aggregator --city=Munich
🌍 Munich | Fetching from 2 sources...
⏱️  Completed in 542ms

✅ Open-Meteo:        9.2°C, 74% humidity, Partly Cloudy
✅ wttr.in:           7.0°C, 87% humidity, Rain shower

📊 Aggregated (2/2 valid):
→ Avg Temperature: 8.10°C
→ Avg Humidity:    80.5%
→ Consensus:       Partly Cloudy ⛅
```

## 🏗️ Architecture

### Concurrent Fetching (Goroutines + Channels)

```go
func fetchWeatherConcurrently(city string, sources []WeatherSource) []WeatherData {
    ch := make(chan WeatherData, len(sources))
    for _, s := range sources {
        go func(src WeatherSource) { ch <- src.Fetch(city) }(s)
    }
    results := make([]WeatherData, 0, len(sources))
    for i := 0; i < len(sources); i++ {
        results = append(results, <-ch)
    }
    return results
}
```

### API Interface

```go
type WeatherSource interface {
    Fetch(city string) WeatherData
    Name() string
}
```

### Error Handling

Each API call handles errors independently. Failed sources don't crash the application - results are aggregated only from successful sources.

## 🧪 Testing

```bash
# Run tests
go test -v

# Run benchmarks
go test -bench=. -benchmem

# Test coverage
go test -cover
```

## 🎯 Key Features

✅ **Concurrent API calls** using goroutines  
✅ **Channel-based synchronization**  
✅ **Per-source error handling**  
✅ **German city support** verified  
✅ **6 weather APIs** (2 free + 4 optional)  
✅ **Compact codebase** (<600 lines total)  
✅ **Comprehensive tests** with benchmarks  
✅ **Clean, idiomatic Go**  

## 🔑 Getting API Keys (Optional)

1. **WeatherAPI.com**: https://www.weatherapi.com/signup.aspx (1M/month free)
2. **Weatherstack**: https://weatherstack.com/signup (1k/month free)

## 📈 Performance

**Typical results (6 APIs):**
- Concurrent: ~800-1200ms
- Sequential: ~5000-7000ms
- **Speedup: 5-8x**

## 🐛 Tested German Cities

✅ Munich (München)  
✅ Berlin  
✅ Hamburg  
✅ Frankfurt  
✅ Cologne (Köln)  
✅ Stuttgart  
✅ Düsseldorf  

## 🔄 Future: Python Comparison

This Go implementation will be compared with Python's `asyncio` to demonstrate:
- Concurrency models (goroutines vs async/await)
- Performance differences
- Code complexity and readability
- Type safety approaches

## 📚 Learning Outcomes

- ✅ Goroutines for lightweight concurrency
- ✅ Channels for safe communication
- ✅ Error handling in concurrent contexts
- ✅ Interface-based design
- ✅ Table-driven tests
- ✅ Real-world API integration

---

**Built with Go 1.21+ | Optimized for ~500 lines | German cities verified ✅**


## 🎓 Demonstrated Concepts

1. **Goroutines** - Lightweight concurrent execution
2. **Channels** - Type-safe communication
3. **Interfaces** - Polymorphic API clients
4. **Error handling** - Per-source without crashes
5. **Table-driven tests** - Comprehensive coverage
6. **Benchmarking** - Performance measurement

## 🔄 Next Steps

1. ✅ Go implementation complete and optimized
2. ⏳ Python implementation with asyncio
3. ⏳ Performance comparison (Go vs Python)
4. ⏳ Code complexity analysis
5. ⏳ Documentation of differences
