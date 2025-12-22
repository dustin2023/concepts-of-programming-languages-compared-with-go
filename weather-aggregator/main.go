package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	// Load environment variables from .env file (ignore errors if file doesn't exist)
	_ = godotenv.Load() // or better os.Getenv?

	// Define and parse command-line flags
	city := flag.String("city", "", "City name (required)")
	seq := flag.Bool("sequential", false, "Use sequential fetching for performance comparison")
	exclude := flag.String("exclude", "", "Comma-separated source names to exclude (e.g., 'wttr.in,WeatherAPI.com')")
	flag.Parse()

	// Validate city input - must not be empty or whitespace-only
	if *city == "" || strings.TrimSpace(*city) == "" {
		fmt.Fprintln(os.Stderr, "Error: City name is required and cannot be empty")
		fmt.Println("\nUsage: weather-aggregator --city=<city> [--sequential] [--exclude=source1,source2]")
		fmt.Println("  --city       City name (required)")
		fmt.Println("  --sequential Use sequential fetching instead of concurrent (optional)")
		fmt.Println("  --exclude    Comma-separated source names to skip (optional)")
		fmt.Println("\nAPI keys are loaded from .env file.")
		fmt.Println("Free sources: Open-Meteo, wttr.in")
		os.Exit(1)
	}

	// Trim whitespace from city name
	cityName := strings.TrimSpace(*city)

	// Initialize all available weather sources
	allSources := initSources()

	// Filter out excluded sources
	excludedMap := make(map[string]bool)
	if *exclude != "" {
		for _, name := range strings.Split(*exclude, ",") {
			excludedMap[strings.TrimSpace(name)] = true
		}
	}

	sources := make([]WeatherSource, 0, len(allSources))
	for _, s := range allSources {
		if !excludedMap[s.Name()] {
			sources = append(sources, s)
		}
	}

	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "Error: All sources were excluded")
		os.Exit(1)
	}

	fmt.Printf("🌍 %s | Fetching from %d sources...\n", cityName, len(sources))

	// Overall timeout for the whole run to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// Measure execution time
	start := time.Now()
	var data []WeatherData

	// Choose execution strategy based on flag
	if *seq {
		// Sequential execution - fetches one after another (for comparison)
		data = fetchSequential(ctx, cityName, sources)
	} else {
		// Concurrent execution - fetches all in parallel using goroutines
		data = fetchWeatherConcurrently(ctx, cityName, sources)
	}
	duration := time.Since(start)

	fmt.Printf("⏱️  Completed in %v\n\n", duration)
	displayResults(data)
}

// initSources creates and returns all available weather sources.
// Free sources (Open-Meteo, wttr.in) are always included.
// API-key-based sources are conditionally added if keys are found in environment.
func initSources() []WeatherSource {
	// Always include free sources
	sources := []WeatherSource{&OpenMeteoSource{}, &WttrinSource{}}

	// Helper function to conditionally add sources based on API key availability
	addSource := func(envKey string, create func(string) WeatherSource) {
		if val := os.Getenv(envKey); val != "" {
			sources = append(sources, create(val))
		}
	}

	// Add optional sources if API keys are available
	addSource("WEATHER_API_COM_KEY", func(k string) WeatherSource { return &WeatherAPISource{k} })
	addSource("WEATHERSTACK_API_KEY", func(k string) WeatherSource { return &WeatherstackSource{k} })
	addSource("METEOSOURCE_API_KEY", func(k string) WeatherSource { return &MeteosourceSource{k} })
	addSource("PIRATE_WEATHER_API_KEY", func(k string) WeatherSource { return &PirateWeatherSource{k} })

	return sources
}

// fetchSequential fetches weather data from all sources one by one.
// This is used for performance comparison with concurrent fetching.
func fetchSequential(ctx context.Context, city string, sources []WeatherSource) []WeatherData {
	results := make([]WeatherData, 0, len(sources))
	for _, s := range sources {
		results = append(results, s.Fetch(ctx, city))
	}
	return results
}

// displayResults prints individual weather data from all sources and aggregated summary.
// Shows per-source results with emoji indicators (✅/❌), duration, and aggregated statistics.
// Aggregation is calculated from all valid responses only.
func displayResults(data []WeatherData) {
	for _, d := range data {
		if d.Error != nil {
			fmt.Printf("❌ %-18s ERROR: %v [%v]\n", d.Source+":", d.Error, d.Duration)
		} else {
			fmt.Printf("✅ %-18s %.1f°C, %.0f%% humidity, %s [%v]\n", d.Source+":", d.Temperature, d.Humidity, d.Condition, d.Duration)
		}
	}

	avgTemp, avgHum, cond, valid := AggregateWeather(data)
	emoji := GetConditionEmoji(cond)

	fmt.Printf("\n📊 Aggregated (%d/%d valid):\n", valid, len(data))
	if valid > 0 {
		fmt.Printf("→ Avg Temperature: %.2f°C\n", avgTemp)
		fmt.Printf("→ Avg Humidity:    %.1f%%\n", avgHum)
		fmt.Printf("→ Consensus:       %s %s\n", cond, emoji)
	} else {
		fmt.Println("→ No valid data available")
	}
}
