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
	// Allow Unicode letters (including umlauts, accents), numbers, spaces, hyphens, apostrophes, and periods
	matched, _ := regexp.MatchString(`^[\p{L}0-9\s\-'\.]+$`, city)
	if !matched {
		return fmt.Errorf("city name contains invalid characters. Use letters (including ü, é, ñ), numbers, spaces, hyphens, apostrophes, and periods")
	}
	return nil
}

func main() {
	// Load .env from parent directory
	_ = godotenv.Load("../.env")

	// Parse command-line flags
	city := flag.String("city", "", "City name (required, spaces allowed)")
	seq := flag.Bool("sequential", false, "Use sequential fetching for performance comparison")
	exclude := flag.String("exclude", "", "Comma-separated source names to exclude (e.g., 'wttr.in,WeatherAPI.com')")
	flag.Parse()

	// Handle multi-word city names from remaining args (e.g., "New York")
	// Only append args that don't look like flags (don't start with -)
	if *city != "" && len(flag.Args()) > 0 {
		for _, arg := range flag.Args() {
			if !strings.HasPrefix(arg, "-") {
				*city = *city + " " + arg
			}
		}
	}

	// Validate city input
	if err := validateCityName(*city); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Println("\nUsage: weather-aggregator --city <city> [OPTIONS]")
		fmt.Println("       weather-aggregator [OPTIONS] --city <city>")
		fmt.Println("\nOptions:")
		fmt.Println("  --city       City name (required)")
		fmt.Println("               • Single word: --city Berlin")
		fmt.Println("               • Multi-word:  --city \"New York\" (quotes recommended)")
		fmt.Println("               • Alternative: --city=New\\ York (escape spaces)")
		fmt.Println("  --sequential Use sequential fetching (optional)")
		fmt.Println("  --exclude    Comma-separated source names to skip (optional)")
		fmt.Println("\nNote: For multi-word cities, place other flags BEFORE --city,")
		fmt.Println("      or use quotes: --sequential --city \"New York\"")
		fmt.Println("\nAPI keys are loaded from .env file.")
		os.Exit(1)
	}

	// Trim whitespace from city name
	cityName := strings.TrimSpace(*city)

	allSources := initSources()

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

	start := time.Now()
	var data []WeatherData

	// Choose execution strategy based on flag
	if *seq {
		data = fetchSequential(ctx, cityName, sources)
	} else {
		data = fetchWeatherConcurrently(ctx, cityName, sources)
	}
	duration := time.Since(start)

	fmt.Printf("⏱️  Completed in %.3fs\n\n", duration.Seconds())
	displayResults(data)
}

// initSources creates all available weather sources.
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

// fetchSequential fetches weather data sequentially for performance comparison.
func fetchSequential(ctx context.Context, city string, sources []WeatherSource) []WeatherData {
	results := make([]WeatherData, 0, len(sources))
	for _, s := range sources {
		results = append(results, s.Fetch(ctx, city))
	}
	return results
}

// displayResults prints per-source results and aggregated statistics.
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
