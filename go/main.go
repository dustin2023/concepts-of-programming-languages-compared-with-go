package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/joho/godotenv"
	"os"
	"regexp"
	"strings"
	"time"
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

// printCityValidationError prints city validation error message and usage.
func printCityValidationError(err error) {
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
}

// parseFlags parses command-line flags and returns parsed city, exclude, and sequential values.
func parseFlags() (city, exclude string, sequential bool) {
	cityFlag := flag.String("city", "", "City name (required, spaces allowed)")
	seqFlag := flag.Bool("sequential", false, "Use sequential fetching for performance comparison")
	excludeFlag := flag.String("exclude", "", "Comma-separated source names to exclude (e.g., 'wttr.in,WeatherAPI.com')")
	flag.Parse()

	// Handle multi-word arguments
	city, exclude = parseMultiWordArgs(*cityFlag, *excludeFlag, seqFlag)
	sequential = *seqFlag

	return city, exclude, sequential
}

// parseMultiWordArgs handles Python argparse-like behavior for multi-word arguments.
// Collects city name parts and exclude parts from unparsed args after flag.Parse().
func parseMultiWordArgs(cityFlag, excludeFlag string, seqFlag *bool) (city, exclude string) {
	cityParts := []string{}
	if cityFlag != "" {
		cityParts = append(cityParts, cityFlag)
	}

	excludeParts := []string{}
	if excludeFlag != "" {
		excludeParts = append(excludeParts, excludeFlag)
	}

	args := flag.Args()

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch {
		case strings.HasPrefix(arg, "--exclude="):
			excludeParts = append(excludeParts, strings.TrimPrefix(arg, "--exclude="))

		case arg == "--exclude":
			// Collect following tokens until next flag
			for j := i + 1; j < len(args) && !strings.HasPrefix(args[j], "--"); j++ {
				excludeParts = append(excludeParts, args[j])
				i = j
			}

		case strings.HasPrefix(arg, "--sequential"):
			*seqFlag = true

		case !strings.HasPrefix(arg, "-"):
			if strings.Contains(arg, ",") {
				excludeParts = append(excludeParts, arg)
			} else {
				cityParts = append(cityParts, arg)
			}
		}
	}

	return strings.Join(cityParts, " "), strings.Join(excludeParts, " ")
}

// displayResults prints per-source results and aggregated statistics.
func displayResults(data []WeatherData) {
	for _, d := range data {
		if d.Error != nil {
			fmt.Printf("❌ %-18s ERROR: %v (%.0fms)\n", d.Source+":", d.Error, d.Duration.Seconds()*1000)
		} else {
			humStr := "N/A"
			if d.Humidity != nil {
				humStr = fmt.Sprintf("%.0f%%", *d.Humidity)
			}
			fmt.Printf("✅ %-18s %.1f°C, %s humidity, %s (%.0fms)\n", d.Source+":", d.Temperature, humStr, d.Condition, d.Duration.Seconds()*1000)
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

// filterExcludedSources removes excluded sources from the list.
func filterExcludedSources(allSources []WeatherSource, exclude string) []WeatherSource {
	if exclude == "" {
		return allSources
	}

	excludedMap := make(map[string]bool)
	for _, name := range strings.Split(exclude, ",") {
		excludedMap[normalizeSourceName(strings.TrimSpace(name))] = true
	}

	sources := make([]WeatherSource, 0, len(allSources))
	for _, s := range allSources {
		if !excludedMap[normalizeSourceName(s.Name())] {
			sources = append(sources, s)
		}
	}
	return sources
}

// runWeatherFetch executes weather fetching with the chosen strategy.
func runWeatherFetch(ctx context.Context, cityName string, sources []WeatherSource, sequential bool) []WeatherData {
	fmt.Printf("🌍 %s | Fetching from %d sources...\n", cityName, len(sources))

	start := time.Now()
	var data []WeatherData

	if sequential {
		data = fetchSequential(ctx, cityName, sources)
	} else {
		data = fetchWeatherConcurrently(ctx, cityName, sources)
	}
	duration := time.Since(start)

	fmt.Printf("⏱️  Completed in %.3fs\n\n", duration.Seconds())
	return data
}

func main() {
	_ = godotenv.Load("../.env")

	if err := loadWeatherCodes(); err != nil {
		fmt.Fprintf(os.Stderr, "Error loading weather codes: %v\n", err)
		os.Exit(1)
	}

	city, exclude, sequential := parseFlags()

	cityName, err := validateCityName(city)
	if err != nil {
		printCityValidationError(err)
		os.Exit(1)
	}

	sources := filterExcludedSources(initSources(), exclude)
	if len(sources) == 0 {
		fmt.Fprintln(os.Stderr, "Error: All sources were excluded")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	data := runWeatherFetch(ctx, cityName, sources, sequential)
	displayResults(data)
}
