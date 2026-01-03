# Weather Data Aggregator: Go vs Python Concurrency Comparison

**Semester Project – Concepts of Programming Languages**  
**Technical Univerity of Applied Science Rosenheim – Winter 2025/2026**

This project demonstrates and compares parallel programming concepts in **Go** and **Python** through a real-world CLI application that fetches weather data from multiple APIs concurrently and aggregates the results.

---

## 📋 Project Overview

A command-line weather aggregator that demonstrates:
- **Concurrent API requests** (6 weather sources in parallel)
- **Language-specific concurrency patterns** (Goroutines vs asyncio)
- **Error-resilient design** (graceful degradation when sources fail)
- **Data aggregation** (temperature, humidity, weather conditions)
- **Real-world HTTP client best practices**
- **International support** (Unicode city names: München, São Paulo, Kraków)

### Key Metrics

| Aspect | Go | Python |
|--------|-----|--------|
| **Concurrency Model** | Goroutines + Channels | asyncio + coroutines |
| **Core LOC** | 692 (main + weather) | 701 (main + weather) |
| **Test LOC** | 224 (incl. 2 integration tests) | 226 (incl. 2 integration tests) |
| **Number of Tests** | 13 unit/integration tests | 14 unit/integration tests |
| **Test Coverage** | 30.4% (core logic focused) | 48% (core logic focused) |
| **Dependencies** | 1 (godotenv) | 3 (aiohttp, python-dotenv, pytest-asyncio) |

---

## 🏗️ Architecture & Execution Flow

### Component Diagram

```mermaid
graph TD
    A[CLI Entry Point] --> B[Input Validator]
    B --> C{Sequential Mode?}
    C -->|No| D[Concurrent Fetcher]
    C -->|Yes| E[Sequential Fetcher]
    D --> F[Weather Sources 1-6]
    E --> F
    F --> G[Geocoding API]
    F --> H[Weather APIs]
    H --> I[Data Aggregator]
    I --> J[Console Output]
    
    style A fill:#e1f5ff
    style D fill:#c8e6c9
    style E fill:#fff9c4
    style I fill:#f8bbd0
    style J fill:#d1c4e9
```

### Execution Flow

1. **Input Validation**: CLI flag `--city` is validated (Unicode-aware regex, length check)
2. **Source Initialization**: Available weather sources loaded (free + API-key sources)
3. **Geocoding** (if needed): City name resolved to latitude/longitude coordinates
4. **Concurrent/Sequential Fetching**:
   - **Concurrent mode** (default): All sources fetched in parallel
   - **Sequential mode** (`--sequential`): Sources fetched one-by-one for comparison
5. **Individual API Requests**: Each source fetches current weather data
6. **Aggregation**: Valid responses aggregated (average temp/humidity, consensus condition)
7. **Display**: Results printed with emojis, timings, and error messages

### Data Flow Sequence

```mermaid
sequenceDiagram
    participant User
    participant CLI
    participant Fetcher
    participant Source1
    participant Source2
    participant Aggregator
    
    User->>CLI: --city=Berlin
    CLI->>Fetcher: fetch_weather_concurrently("Berlin")
    
    par Parallel Execution
        Fetcher->>Source1: fetch("Berlin")
        Fetcher->>Source2: fetch("Berlin")
    end
    
    Source1-->>Fetcher: WeatherData(temp=15°C, ...)
    Source2-->>Fetcher: WeatherData(temp=16°C, ...)
    
    Fetcher->>Aggregator: aggregate([data1, data2])
    Aggregator-->>CLI: avg_temp, avg_hum, condition
    CLI-->>User: Display results
```

### Concurrency Model Comparison

**Go: Goroutines + Channels**

```mermaid
graph LR
    Main[Main Thread] --> G1[Goroutine 1<br/>Open-Meteo]
    Main --> G2[Goroutine 2<br/>Tomorrow.io]
    Main --> G3[Goroutine 3<br/>WeatherAPI]
    Main --> G4[Goroutine 4<br/>...]
    Main --> G5[Goroutine 5<br/>...]
    Main --> G6[Goroutine 6<br/>Pirate Weather]
    
    G1 -->|Send Result| CH[Buffered Channel<br/>Size: 6]
    G2 -->|Send Result| CH
    G3 -->|Send Result| CH
    G4 -->|Send Result| CH
    G5 -->|Send Result| CH
    G6 -->|Send Result| CH
    
    CH -->|Receive 6x| Collect[Collect Results]
    Collect --> Agg[Aggregate]
    
    style Main fill:#e3f2fd
    style CH fill:#fff9c4
    style Collect fill:#c8e6c9
    style Agg fill:#f8bbd0
```

**Python: asyncio Event Loop**

```mermaid
graph LR
    Main[Main Coroutine] --> T1[Task 1<br/>Open-Meteo]
    Main --> T2[Task 2<br/>Tomorrow.io]
    Main --> T3[Task 3<br/>WeatherAPI]
    Main --> T4[Task 4<br/>...]
    Main --> T5[Task 5<br/>...]
    Main --> T6[Task 6<br/>Pirate Weather]
    
    T1 -->|await| EL[Event Loop<br/>Single Thread]
    T2 -->|await| EL
    T3 -->|await| EL
    T4 -->|await| EL
    T5 -->|await| EL
    T6 -->|await| EL
    
    EL -->|asyncio.gather| Collect[Collect All Results]
    Collect --> Agg[Aggregate]
    
    style Main fill:#e1f5ff
    style EL fill:#fff3e0
    style Collect fill:#c8e6c9
    style Agg fill:#f8bbd0
```

**Key Differences:**
- **Go**: True parallelism with M:N threading (goroutines scheduled across OS threads)
- **Python**: Cooperative multitasking on single thread (switches during I/O waits)
- **Go**: Channels for communication (CSP model)
- **Python**: Futures/Promises with `await` (async/await model)

---

## 📂 Repository Structure

```
├── go/                      # Go implementation
│   ├── main.go              # CLI entry point
│   ├── weather.go           # Core logic, API clients, coord caching
│   ├── main_test.go         # CLI validation tests
│   ├── weather_test.go      # Unit + integration tests
│   ├── go.mod / go.sum      # Dependencies
│
├── python/                  # Python implementation
│   ├── main.py              # CLI entry point
│   ├── weather.py           # Core logic, API clients
│   ├── test_weather.py      # Unit + integration tests
│   ├── requirements.txt     # Dependencies
│   └── venv/                # Virtual environment (gitignored)
│
├── weather_codes.json       # Shared weather code mappings
├── .env.example             # API key template
├── .gitignore
└── README.md
```

---

## 🚀 Quick Start

### Prerequisites
- **Go** 1.21 or higher
- **Python** 3.11 or higher
- API keys (optional – free sources work without keys)
- **Terminal with UTF-8 support** (for Unicode city names)

### Setup

```bash
# Clone the repository
git clone <repo-url>
cd concepts-of-programming-languages-compared-with-go

# Copy and configure API keys (optional)
cp .env.example .env
# Edit .env with your API keys (see below for providers)
```

### Running the Go Version

```bash
cd go
go build -o weather-aggregator
./weather-aggregator --city Berlin

# Multi-word cities work without quotes:
./weather-aggregator --city New York
./weather-aggregator --city St. Gallen

# Unicode support (umlauts, accents, etc.):
./weather-aggregator --city München
./weather-aggregator --city "Saint-Étienne"
./weather-aggregator --city Zürich

# Options:
./weather-aggregator --city Munich --sequential  # Sequential mode
./weather-aggregator --city London --exclude Weatherstack,Meteosource
```

### Running the Python Version

```bash
cd python

# Create virtual environment (first time only)
python3 -m venv venv
source venv/bin/activate

# Install dependencies
pip install -r requirements.txt

# Run the application
python main.py --city Berlin

# Multi-word cities (spaces allowed, quotes optional):
python main.py --city New York
python main.py --city "St. Gallen"

# Unicode support (umlauts, accents, etc.):
python main.py --city München
python main.py --city São Paulo
python main.py --city Kraków

# Options:
python main.py --city Munich --sequential
python main.py --city London --exclude Weatherstack,Meteosource
```

---

## 🧪 Running Tests

### Go Tests

```bash
cd go

# Run all tests with coverage
go test -v -cover

# Run tests with detailed coverage report
go test -v -coverprofile=coverage.out
go tool cover -html=coverage.out

# Run specific test
go test -v -run TestAggregateWeather
```

- Covers: Aggregation logic, validation, condition mapping, mock concurrency

### Python Tests

```bash
cd python
source venv/bin/activate

# Run all tests
python3 -m pytest test_weather.py -v

# Run with coverage
python3 -m pytest test_weather.py -v --cov=weather --cov-report=html

# Run specific test
python3 -m pytest test_weather.py::test_normalize_condition -v
```

- Covers: Aggregation, normalization, validation, async mocking

---

## Concurrency Comparison

### Core Concurrency Patterns

#### Go: Goroutines + Channels (CSP Model)

```go
func fetchWeatherConcurrently(ctx context.Context, city string, sources []WeatherSource) []WeatherData {
    // Buffered channel to collect results
    ch := make(chan WeatherData, len(sources))
    
    // Launch goroutines for each source
    for _, s := range sources {
        go func(src WeatherSource) {
            ch <- src.Fetch(ctx, city)  // Send result to channel
        }(s)
    }
    
    // Collect all results
    results := make([]WeatherData, 0, len(sources))
    for i := 0; i < len(sources); i++ {
        results = append(results, <-ch)  // Receive from channel
    }
    return results
}
```

**Key Features:**
- Lightweight goroutines (thousands possible)
- Channels for communication (CSP pattern)
- No explicit event loop
- Synchronous-looking async code

#### Python: asyncio + Coroutines (Event Loop)

```python
async def fetch_weather_concurrently(city: str, sources: List[WeatherSource]) -> List[WeatherData]:
    async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:
        coords_cache: Dict[str, Tuple[float, float]] = {}
        
        # Pre-fetch coordinates
        coords, err = await geocode_city(city, session)
        if not err:
            coords_cache[city] = coords
        
        # Gather all tasks concurrently
        tasks = [
            _fetch_with_timing(source, city, session, coords_cache)
            for source in sources
        ]
        return await asyncio.gather(*tasks)
```

**Key Features:**
- Single-threaded event loop
- `async/await` syntax for coroutines
- Explicit `gather()` for parallelism
- Requires async-compatible libraries

### Performance Comparison

**Methodology:**
- Tested with 6 weather sources (5 active, 1 rate-limited)
- City: Berlin (Germany)
- Date: January 2026
- Network: Stable broadband connection
- Hardware: Apple M1 Pro
- Measured: Wall-clock time from start to completion
- **Optimization**: Both implementations now cache coordinates to reduce redundant geocoding calls

| Mode | Go | Python | Speedup |
|------|-----|--------|---------||
| **Concurrent** | 1.64s | 0.99s | Baseline |
| **Sequential** | 2.17s | 2.39s | Baseline |
| **Improvement** | 1.3x | 2.4x | **Python faster in concurrent mode** |

**Observations:**
- Python's asyncio achieves better concurrent performance (0.99s vs 1.64s)
- Go's sequential mode is slightly faster (2.17s vs 2.39s)
- Python benefits more from concurrency (2.4x speedup vs Go's 1.3x)
- Real bottleneck remains network latency (API response times)
- **Coordinate caching** eliminates redundant geocoding calls, improving fairness

---

## 🌐 Weather Data Sources

The application supports 6 weather APIs with automatic fallback:

| Source | API Key | Free Tier | Notes |
|--------|---------|-----------|-------|
| **Open-Meteo** | ❌ No | Unlimited | Free geocoding + weather |
| **Tomorrow.io** | ✅ Yes | 500 calls/day | Requires coordinates |
| **WeatherAPI.com** | ✅ Yes | 1M calls/month | Most generous free tier |
| **Weatherstack** | ✅ Yes | 1k calls/month | HTTP only (no HTTPS) |
| **Meteosource** | ✅ Yes | Limited | May lack humidity data |
| **Pirate Weather** | ✅ Yes | 1k calls/month | Dark Sky compatible |

### API Key Setup

Get your free API keys:
- **WeatherAPI.com**: https://www.weatherapi.com/signup.aspx
- **Weatherstack**: https://weatherstack.com/signup/free
- **Meteosource**: https://www.meteosource.com/client/sign-up
- **Pirate Weather**: https://pirateweather.net/
- **Tomorrow.io**: https://www.tomorrow.io/weather-api/

Add them to `.env`:
```bash
WEATHER_API_COM_KEY=your_key_here
WEATHERSTACK_API_KEY=your_key_here
METEOSOURCE_API_KEY=your_key_here
PIRATE_WEATHER_API_KEY=your_key_here
TOMORROW_API_KEY=your_key_here
```

---

## Key Programming Concepts Demonstrated

### 1. Concurrency Models

**Go: Communicating Sequential Processes (CSP)**
- Goroutines as lightweight threads
- Channels for inter-goroutine communication
- `select` statement for channel multiplexing
- No shared memory (message passing)

**Python: Event Loop with Coroutines**
- Single-threaded cooperative multitasking
- `async/await` syntax for coroutine definition
- `asyncio.gather()` for concurrent execution
- Explicit task scheduling

### 2. Interface/Protocol Abstraction

**Go:**
```go
type WeatherSource interface {
    Fetch(ctx context.Context, city string) WeatherData
    Name() string
}
```
- Implicit interface satisfaction
- Duck typing at compile time
- Structural subtyping

**Python:**
```python
class WeatherSource(Protocol):
    name: str
    async def fetch(self, city: str, session: ClientSession, 
                    coords_cache: Optional[Dict]) -> WeatherData: ...
```
- Explicit Protocol from `typing`
- Runtime duck typing
- Type hints for static analysis

### 3. Error Handling Patterns

**Go:**
- Explicit error returns (`error` type)
- Error wrapping with `fmt.Errorf("%w", err)`
- Early return pattern
- No exceptions
- Unicode-aware input validation

**Python:**
- Custom exceptions for error flow
- `try/except` blocks for error handling
- `Optional` types for nullable values
- Graceful degradation with partial results
- Unicode-aware regex validation

### 4. HTTP Client Best Practices

**Both implementations:**
- Connection pooling and reuse
- Request timeouts (10s per request)
- Context cancellation (Go) / Task cancellation (Python)
- Proper resource cleanup (`defer`/`async with`)
- User-Agent headers
- Error categorization (timeout, network, HTTP errors)

### 5. Testing Strategies

**Go: Table-Driven Tests**
```go
tests := []struct {
    name string
    data []WeatherData
    want int
}{
    {"all valid", validData, 2},
    {"mixed errors", mixedData, 1},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        // test logic
    })
}
```

**Python: Parametrized Tests**
```python
@pytest.mark.parametrize("city,expected", [
    ("Munich", "Munich"),
    ("  Berlin  ", "Berlin"),
    ("", None),
])
def test_validate_city_name(city, expected):
    assert validate_city_name(city) == expected
```

---


## 🔍 Language Comparison Analysis

### Strengths of Go for This Use Case

✅ **Simpler concurrency model**: Goroutines "just work" without async/await
✅ **Better performance**: ~15% faster in concurrent mode
✅ **Smaller binary**: Single compiled executable
✅ **Easier deployment**: No virtual environment needed
✅ **Better tooling**: Built-in testing, benchmarking, coverage

### Strengths of Python for This Use Case

✅ **More expressive**: Type hints, dataclasses, protocols
✅ **Better ecosystem**: More weather libraries available
✅ **Faster development**: Less boilerplate code
✅ **Dynamic typing**: Easier to handle varying API responses
✅ **Better REPL**: Interactive development and debugging
✅ **Native Unicode**: Built-in Unicode support without extra configuration

### Trade-offs

| Aspect | Go | Python |
|--------|-----|--------|
| **Startup time** | <10ms | ~100ms (import overhead) |
| **Memory usage** | ~15MB | ~40MB (interpreter + libs) |
| **Learning curve** | Steeper (channels, goroutines) | Easier (familiar async/await) |
| **Type safety** | Compile-time | Runtime (with type hints) |
| **Deployment** | Single binary | Requires Python + dependencies |

---

## 📚 References & Resources

### Concurrency Patterns
- Pike, Rob. *"Concurrency is not Parallelism"*. https://go.dev/blog/waza-talk
- *Go Concurrency Patterns*. https://go.dev/talks/2012/concurrency.slide
- *Python asyncio Documentation*. https://docs.python.org/3/library/asyncio.html
- Beazley, David. *"Python Concurrency From the Ground Up"*. PyCon 2015

### HTTP Client Best Practices
- *Go net/http Package Documentation*. https://pkg.go.dev/net/http
- *aiohttp Documentation*. https://docs.aiohttp.org/
- *HTTP Timeouts in Go*. https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/

### Testing
- *Go Testing Package*. https://pkg.go.dev/testing
- *pytest Documentation*. https://docs.pytest.org/
- *Table-Driven Tests in Go*. https://dave.cheney.net/2019/05/07/prefer-table-driven-tests

### Weather APIs
- Open-Meteo API: https://open-meteo.com/
- WeatherAPI.com: https://www.weatherapi.com/docs/
- WMO Weather Codes: https://www.nodc.noaa.gov/archive/arc0021/0002199/1.1/data/0-data/HTML/WMO-CODE/WMO4677.HTM

### Course Materials
- *Concepts of Programming Languages*, Hochschule Rosenheim

---

## Known Limitations & Future Work

### Current Limitations

1. **No Result Caching**: Each run fetches fresh data from all APIs, even for repeated queries. This wastes API quota and bandwidth.
2. **Limited Error Recovery**: When geocoding fails, individual sources retry independently. A shared fallback strategy would be more efficient.
3. **Basic Aggregation**: Weather condition consensus uses simple majority voting. More sophisticated algorithms (weighted by source reliability) could improve accuracy.
4. **No Historical Data**: Only current weather is fetched. No support for forecasts or historical trends.
5. **Rate Limiting**: Some free-tier APIs (e.g., Weatherstack) impose strict rate limits. The application does not implement exponential backoff or retry logic.
6. **Test Coverage**: While core aggregation logic is well-tested (31-48%), HTTP error scenarios and edge cases could benefit from more comprehensive testing.

### Potential Improvements

1. **Result Caching**: Implement in-memory or file-based cache (TTL: 5-10 minutes) to reduce redundant API calls
2. **Weighted Aggregation**: Assign reliability scores to sources based on historical accuracy, use weighted average for temperature
3. **Forecast Support**: Extend API to fetch 3-day/7-day forecasts, add visualization (ASCII graphs or web UI)
4. **Retry Logic**: Implement exponential backoff for transient errors (429, 503), with configurable max retries
5. **Configuration File**: Support YAML/TOML config for customizing timeouts, excluded sources, and cache settings
6. **Observability**: Add structured logging (JSON), metrics export (Prometheus format), and health check endpoint
7. **Database Storage**: Store weather history in SQLite/PostgreSQL for trend analysis and comparison
8. **WebAssembly Port**: Compile Go version to WASM for browser-based usage without backend

---

**Author**: Dustin  
**Date**: January 2026  
**Course**: Concepts of Programming Languages, Technical Univerity of Applied Science Rosenheim




