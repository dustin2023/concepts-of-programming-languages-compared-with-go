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
	addSource("METEOSOURCE_API_KEY", func(k string) WeatherSource { return &MeteosourceSource{k} })
	addSource("PIRATE_WEATHER_API_KEY", func(k string) WeatherSource { return &PirateWeatherSource{k} })

	return sources
}

func validateCityName(city string) (string, error) {
	trimmed := strings.TrimSpace(city)
	if trimmed == "" {
		return "", fmt.Errorf("city name is required and cannot be empty")
	}
	// Prevent flag-like values (aligns behavior with Python CLI parsing)
	if strings.HasPrefix(trimmed, "-") {
		return "", fmt.Errorf("city name cannot start with '-'; did you mean to pass a flag?")
	}
	if len(trimmed) < 2 {
		return "", fmt.Errorf("city name must be at least 2 characters long")
	}
	if len(trimmed) > 100 {
		return "", fmt.Errorf("city name must not exceed 100 characters")
	}
	// Allow Unicode letters (including umlauts, accents), numbers, spaces, hyphens, apostrophes, and periods
	matched, _ := regexp.MatchString(`^[\p{L}0-9\s\-'\.]+$`, trimmed)
	if !matched {
		return "", fmt.Errorf("city name contains invalid characters. Use letters (including ü, é, ñ), numbers, spaces, hyphens, apostrophes, and periods")
	}
	return trimmed, nil
}

func main() {
	_ = godotenv.Load("../.env")

	// Parse command-line flags
	city := flag.String("city", "", "City name (required, spaces allowed)")
	seq := flag.Bool("sequential", false, "Use sequential fetching for performance comparison")
	exclude := flag.String("exclude", "", "Comma-separated source names to exclude (e.g., 'wttr.in,WeatherAPI.com')")
	flag.Parse()

	// Collect city name parts and manually parse flags that appear after positional args
	// This enables Python argparse-like behavior: --city New York --exclude Source
	cityParts := []string{}
	if *city != "" {
		cityParts = append(cityParts, *city)
	}
	
	for i := 0; i < len(flag.Args()); i++ {
		arg := flag.Args()[i]
		if strings.HasPrefix(arg, "--exclude=") {
			*exclude = strings.TrimPrefix(arg, "--exclude=")
		} else if arg == "--exclude" && i+1 < len(flag.Args()) {
			i++
			*exclude = flag.Args()[i]
		} else if strings.HasPrefix(arg, "--sequential") {
			*seq = true
		} else if !strings.HasPrefix(arg, "-") {
			cityParts = append(cityParts, arg)
		}
	}
	
	if len(cityParts) > 0 {
		*city = strings.Join(cityParts, " ")
	}

	// Validate city input
	cityName, err := validateCityName(*city)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		fmt.Println("\nUsage: weather-aggregator --city <city> [OPTIONS]")
		fmt.Println("\nOptions:")
		fmt.Println("  --city       City name (required)")
		fmt.Println("  --sequential Use sequential fetching (optional)")
		fmt.Println("  --exclude    Comma-separated source names to skip (optional)")
		fmt.Println("\nExamples:")
		fmt.Println("  ./weather-aggregator --city New York")
		fmt.Println("  ./weather-aggregator --city Berlin --exclude WeatherAPI.com")
		fmt.Println("  ./weather-aggregator --city \"São Paulo\" --sequential")
		fmt.Println("\nAPI keys are loaded from .env file.")
		os.Exit(1)
	}

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
