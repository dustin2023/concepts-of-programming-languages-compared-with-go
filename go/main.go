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

func validateCityName(city string) (string, error) {
	trimmed := strings.TrimSpace(city)
	if trimmed == "" {
		return "", fmt.Errorf("city name is required and cannot be empty")
	}
	if strings.HasPrefix(trimmed, "-") {
		return "", fmt.Errorf("city name cannot start with '-'")
	}
	if len(trimmed) > 100 {
		return "", fmt.Errorf("city name must not exceed 100 characters")
	}
	// Allow Unicode letters, digits, spaces, any dash, apostrophes, periods, underscore
	matched, _ := regexp.MatchString(`^[\p{L}0-9_\s\p{Pd}'\.]+$`, trimmed)
	if !matched {
		return "", fmt.Errorf("Invalid city name. Allowed: letters (ü, é, ñ), digits, spaces, hyphens, apostrophes, periods")
	}
	return trimmed, nil
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

	excludeParts := []string{}
	for i := 0; i < len(flag.Args()); i++ {
		arg := flag.Args()[i]

		switch {
		case strings.HasPrefix(arg, "--exclude="):
			val := strings.TrimPrefix(arg, "--exclude=")
			excludeParts = append(excludeParts, val)

		case arg == "--exclude":
			// Collect following tokens until next flag
			for j := i + 1; j < len(flag.Args()) && !strings.HasPrefix(flag.Args()[j], "--"); j++ {
				excludeParts = append(excludeParts, flag.Args()[j])
				i = j
			}

		case strings.HasPrefix(arg, "--sequential"):
			*seq = true

		case !strings.HasPrefix(arg, "-"):
			if strings.Contains(arg, ",") {
				excludeParts = append(excludeParts, arg)
			} else {
				cityParts = append(cityParts, arg)
			}
		}
	}

	if len(excludeParts) > 0 {
		*exclude = strings.Join(excludeParts, " ")
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
		fmt.Println("  ./weather-aggregator --city \"O'Brien\"    # apostrophe needs double-quotes in the shell")
		fmt.Println("  ./weather-aggregator --city Berlin --exclude WeatherAPI.com")
		fmt.Println("\nAPI keys are loaded from .env file.")
		os.Exit(1)
	}

	// Load weather code mappings once (needed for all sources, not nur Open-Meteo)
	if err := loadWeatherCodes(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading weather codes: %v\n", err)
		os.Exit(1)
	}

	allSources := initSources()

	excludedMap := make(map[string]bool)
	if *exclude != "" {
		for _, name := range strings.Split(*exclude, ",") {
			n := strings.TrimSpace(name)
			excludedMap[normalizeSourceName(n)] = true
		}
	}

	sources := make([]WeatherSource, 0, len(allSources))
	for _, s := range allSources {
		if !excludedMap[normalizeSourceName(s.Name())] {
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
