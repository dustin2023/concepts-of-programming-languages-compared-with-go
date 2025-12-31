# Parallel Programming: Go vs Python

**Semester Project – Concepts of Programming Languages**

This project demonstrates and compares parallel programming concepts in **Go** and **Python** through a real-world CLI application: a Weather Data Aggregator that fetches data from multiple APIs concurrently.

## 📋 Project Overview

| Aspect | Go | Python |
|--------|-----|--------|
| **Concurrency Model** | Goroutines + Channels | asyncio + coroutines |
| **Lines of Code** | ~820 | ~890 |
| **Tests** | 4 tests + 2 benchmarks | 33 tests |
| **Dependencies** | 1 (godotenv) | 3 (aiohttp, python-dotenv, pytest-asyncio) |

## 🏗️ Repository Structure

```
├── go/                      # Go implementation
│   ├── main.go              # CLI entry point
│   ├── weather.go           # Core logic, API clients, aggregation
│   ├── weather_test.go      # Unit tests and benchmarks
│   ├── go.mod / go.sum      # Dependencies
│
├── python/                  # Python implementation
│   ├── main.py              # CLI entry point
│   ├── weather.py           # Core logic, API clients, aggregation
│   ├── test_weather.py      # Unit tests (pytest)
│   ├── requirements.txt     # Dependencies
│   └── venv/                # Virtual environment (gitignored)
│
├── .env.example             # API key template
├── .gitignore
└── README.md
```

## 🚀 Quick Start

### Prerequisites
- Go 1.21+
- Python 3.11+
- API keys (optional, free sources work without keys)

### Setup

```bash
# Clone and setup
git clone <repo-url>
cd concepts-of-programming-languages-compared-with-go

# Copy and configure API keys
cp .env.example go/.env
cp .env.example python/.env
# Edit .env files with your API keys
```

### Run Go Version

```bash
cd go
go build -o weather-aggregator
./weather-aggregator --city=Berlin

# Sequential mode for comparison
./weather-aggregator --city=Berlin --sequential
```

### Run Python Version

```bash
cd python
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt

python main.py --city=Berlin

# Sequential mode for comparison
python main.py --city=Berlin --sequential
```

## 🧪 Running Tests

### Go
```bash
cd go
go test -v -cover              # Run tests with coverage
go test -bench=. -benchmem     # Run benchmarks
```

### Python
```bash
cd python
source venv/bin/activate
python -m pytest test_weather.py -v
```

## ⚡ Concurrency Comparison

### Go: Goroutines + Channels
```go
func fetchWeatherConcurrently(ctx context.Context, city string, sources []WeatherSource) []WeatherData {
    ch := make(chan WeatherData, len(sources))
    for _, s := range sources {
        go func(src WeatherSource) { ch <- src.Fetch(ctx, city) }(s)
    }
    results := make([]WeatherData, 0, len(sources))
    for i := 0; i < len(sources); i++ {
        results = append(results, <-ch)
    }
    return results
}
```

### Python: asyncio.gather
```python
async def fetch_weather_concurrently(city: str, sources: List[WeatherSource]) -> List[WeatherData]:
    tasks = [source.fetch(city) for source in sources]
    results = await asyncio.gather(*tasks, return_exceptions=False)
    return list(results)
```

### Performance Results (6 sources)

| Mode | Go | Python |
|------|-----|--------|
| **Concurrent** | ~3-4s | ~3-5s |
| **Sequential** | ~8-11s | ~10-12s |
| **Speed-up** | ~2.5x | ~2.2x |

## 🌐 Weather Sources

| Source | API Key Required | Notes |
|--------|------------------|-------|
| Open-Meteo | ❌ No | Free geocoding + weather |
| wttr.in | ❌ No | Free, simple API |
| WeatherAPI.com | ✅ Yes | 1M calls/month free |
| Weatherstack | ✅ Yes | HTTP only on free tier |
| Meteosource | ✅ Yes | No humidity on free tier |
| Pirate Weather | ✅ Yes | Dark Sky compatible |

## ✅ Academic Requirements Checklist

| Requirement | Status |
|-------------|--------|
| Non-trivial program demonstrating semester topics | ✅ 6 APIs, concurrent fetching, aggregation |
| Console I/O (input and output) | ✅ CLI flags, formatted output |
| Proper error handling (no crashes) | ✅ All errors caught and displayed |
| Core logic covered by tests | ✅ Unit tests for aggregation, normalization |
| Minimal dependencies | ✅ Go: 1, Python: 3 |
| Clear presentation possible | ✅ Modular code, performance demo |
| Implemented in both languages | ✅ Go (820 LOC) + Python (890 LOC) |
| Code quality priority | ✅ Clean, documented, idiomatic |
| English code and comments | ✅ All English |

## 📊 Key Concepts Demonstrated

1. **Concurrency Patterns**
   - Go: CSP model with goroutines and channels
   - Python: Async/await with event loop

2. **Interface/Protocol Abstraction**
   - Go: `WeatherSource` interface
   - Python: `WeatherSource` Protocol (typing)

3. **Error Handling**
   - Go: Error values, early returns
   - Python: Exceptions, Optional types

4. **HTTP Client Best Practices**
   - Timeouts, context cancellation
   - Connection reuse, proper cleanup

5. **Testing Strategies**
   - Table-driven tests (Go)
   - Parametrized tests (Python/pytest)
   - Mock sources for unit testing

## 📝 License

Academic project – Hochschule Rosenheim



