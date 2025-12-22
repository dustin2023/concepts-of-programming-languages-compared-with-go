package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/joho/godotenv"
)

func main() {
	_ = godotenv.Load()
	city := flag.String("city", "", "City name (required)")
	seq := flag.Bool("sequential", false, "Use sequential fetching")
	flag.Parse()

	if *city == "" {
		fmt.Println("Usage: weather-aggregator --city=<city> [--sequential]")
		fmt.Println("API keys are loaded from .env file.")
		os.Exit(1)
	}

	sources := initSources()
	fmt.Printf("🌍 %s | Fetching from %d sources...\n", *city, len(sources))

	start := time.Now()
	var data []WeatherData
	if *seq {
		data = fetchSequential(*city, sources)
	} else {
		data = fetchWeatherConcurrently(*city, sources)
	}
	duration := time.Since(start)

	fmt.Printf("⏱️  Completed in %v\n\n", duration)
	displayResults(data)
}

func initSources() []WeatherSource {
	sources := []WeatherSource{&OpenMeteoSource{}, &WttrinSource{}}

	addSource := func(envKey string, create func(string) WeatherSource) {
		if val := os.Getenv(envKey); val != "" {
			sources = append(sources, create(val))
		}
	}

	addSource("WEATHER_API_COM_KEY", func(k string) WeatherSource { return &WeatherAPISource{k} })
	addSource("OPENWEATHER_API_KEY", func(k string) WeatherSource { return &OpenWeatherSource{k} })
	addSource("WEATHERSTACK_API_KEY", func(k string) WeatherSource { return &WeatherstackSource{k} })
	addSource("VISUALCROSSING_API_KEY", func(k string) WeatherSource { return &VisualCrossingSource{k} })
	addSource("METEOSOURCE_API_KEY", func(k string) WeatherSource { return &MeteosourceSource{k} })
	addSource("PIRATE_WEATHER_API_KEY", func(k string) WeatherSource { return &PirateWeatherSource{k} })

	return sources
}

func fetchSequential(city string, sources []WeatherSource) []WeatherData {
	results := make([]WeatherData, 0, len(sources))
	for _, s := range sources {
		results = append(results, s.Fetch(city))
	}
	return results
}

func displayResults(data []WeatherData) {
	for _, d := range data {
		if d.Error != nil {
			fmt.Printf("❌ %-18s ERROR: %v\n", d.Source+":", d.Error)
		} else {
			fmt.Printf("✅ %-18s %.1f°C, %.0f%% humidity, %s\n", d.Source+":", d.Temperature, d.Humidity, d.Condition)
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
