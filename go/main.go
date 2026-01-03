package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

func validateCityName(city string) error {
	trimmed := strings.TrimSpace(city)
	if trimmed == "" {
		return fmt.Errorf("city name is required and cannot be empty")
	}
	if len(trimmed) < 2 {
		return fmt.Errorf("city name must be at least 2 characters long")
	}
	if len(city) > 100 {
		return fmt.Errorf("city name must not exceed 100 characters")
	}
	// Allow letters, numbers, spaces, hyphens, and apostrophes (common in city names)
	matched, _ := regexp.MatchString(`^[a-zA-Z0-9\s\-']+$`, city)
	if !matched {
		return fmt.Errorf("city name contains invalid characters. Use letters, numbers, spaces, hyphens, and apostrophes")
	}
	return nil
}

func main() {
	// Load environment variables from .env file (ignore errors if file doesn't exist)
	// use .env from parent directory
	_ = godotenv.Load("../.env")

	// Define and parse command-line flags
	city := flag.String("city", "", "City name (required)")
	seq := flag.Bool("sequential", false, "Use sequential fetching for performance comparison")
	exclude := flag.String("exclude", "", "Comma-separated source names to exclude (e.g., 'wttr.in,WeatherAPI.com')")
	flag.Parse()

	// Validate city input using dedicated validation function
	if err := validateCityName(*city); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Println("\nUsage: weather-aggregator --city=<city> [--sequential] [--exclude=source1,source2]")
		fmt.Println("  --city       City name (required)")
		fmt.Println("  --sequential Use sequential fetching instead of concurrent (optional)")
		fmt.Println("  --exclude    Comma-separated source names to skip (optional)")
		fmt.Println("\nAPI keys are loaded from .env file.")
		fmt.Println("Free sources: Open-Meteo")
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

	fmt.Printf("⏱️  Completed in %.3fs\n\n", duration.Seconds())
	displayResults(data)
}

// initSources creates and returns all available weather sources.
func initSources() []WeatherSource {
	sources := []WeatherSource{&OpenMeteoSource{}}

	addSource := func(envKey string, create func(string) WeatherSource) {
		if val := os.Getenv(envKey); val != "" {
			sources = append(sources, create(val))
		}
	}

	addSource("TOMORROW_API_KEY", func(k string) WeatherSource { return &TomorrowIOSource{apiKey: k} })
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
			fmt.Printf("❌ %-18s ERROR: %v [%.0fms]\n", d.Source+":", d.Error, d.Duration.Seconds()*1000)
		} else {
			humStr := "N/A"
			if d.Humidity != nil {
				humStr = fmt.Sprintf("%.0f%%", *d.Humidity)
			}
			fmt.Printf("✅ %-18s %.1f°C, %s humidity, %s [%.0fms]\n", d.Source+":", d.Temperature, humStr, d.Condition, d.Duration.Seconds()*1000)
		}
	}

	avgTemp, avgHum, cond, valid := AggregateWeather(data)
	emoji := GetConditionEmoji(cond)

	fmt.Printf("\n📊 Aggregated (%d/%d valid):\n", valid, len(data))
	if valid > 0 {
		fmt.Printf("→ Avg Temperature: %.2f°C\n", avgTemp)
		if avgHum > 0 {
			fmt.Printf("→ Avg Humidity:    %.1f%%\n", avgHum)
		} else {
			fmt.Printf("→ Avg Humidity:    N/A\n")
		}
		fmt.Printf("→ Consensus:       %s %s\n", cond, emoji)
	} else {
		fmt.Println("→ No valid data available")
	}
}
