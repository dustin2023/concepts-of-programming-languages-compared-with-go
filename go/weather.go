package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// WeatherCodeRange represents a range of weather codes mapped to a condition.
type WeatherCodeRange struct {
	Min       int    `json:"min"`
	Max       int    `json:"max"`
	Condition string `json:"condition"`
}

// WeatherCodeConfig represents the structure of weather_codes.json.
type WeatherCodeConfig struct {
	WMO struct {
		Ranges []WeatherCodeRange `json:"ranges"`
	} `json:"wmo"`
	TomorrowIO map[string]string `json:"tomorrow_io"`
}

// WeatherCodes holds the unified weather code mappings loaded from JSON.
var WeatherCodes WeatherCodeConfig

func init() {
	// Load weather code mappings from shared JSON file.
	// This is critical configuration - if it fails, the program cannot function correctly.
	path := filepath.Join("..", "weather_codes.json")
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Could not load weather_codes.json: %v\n", err)
		fmt.Fprintf(os.Stderr, "Program requires this file to map weather codes correctly.\n")
		os.Exit(1)
	}
	if err := json.Unmarshal(data, &WeatherCodes); err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: Invalid weather_codes.json format: %v\n", err)
		fmt.Fprintf(os.Stderr, "Please ensure the JSON file is valid and properly formatted.\n")
		os.Exit(1)
	}
}

// WeatherData represents the weather information from a single source.
// Temperature is in Celsius, Humidity is a percentage (0-100).
// Humidity pointer allows distinguishing between 0% humidity and missing humidity data.
// Error field contains any error that occurred during fetching.
// Duration measures the time taken to fetch from this source.
type WeatherData struct {
	Source      string
	Temperature float64
	Humidity    *float64 // Pointer to distinguish between 0% and missing data
	Condition   string
	Error       error
	Duration    time.Duration
}

// WeatherSource is the interface that all weather API implementations must satisfy.
// Each source knows how to fetch weather data for a given city.
type WeatherSource interface {
	Fetch(ctx context.Context, city string) WeatherData
	Name() string
}

// fetchWeatherConcurrently fetches weather data from all sources in parallel using goroutines.
// It creates a buffered channel, launches a goroutine for each source, and collects results.
// This demonstrates Go's concurrency model with goroutines and channels.
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

// client is a shared HTTP client with a 10-second timeout for all API requests.
// Using a shared client improves performance through connection reuse.
// Go's DefaultTransport already has sensible defaults for connection pooling.
var client = &http.Client{
	Timeout: 10 * time.Second,
}

// doGet creates a request with context, sets a simple UA, checks non-200 early, and returns the response.
// Returns the response and duration of the entire request.
func doGet(ctx context.Context, url string) (*http.Response, time.Duration, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, time.Since(start), fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "weather-aggregator/1.0")

	resp, err := client.Do(req)
	duration := time.Since(start)
	if err != nil {
		return nil, duration, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, duration, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	return resp, duration, nil
}

// lookupLatLon resolves a city name to coordinates using Open-Meteo geocoding.
func lookupLatLon(ctx context.Context, city string) (float64, float64, error) {
	geoURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1", url.QueryEscape(city))
	resp, _, err := doGet(ctx, geoURL)
	if err != nil {
		return 0, 0, fmt.Errorf("geocoding: %w", err)
	}
	defer resp.Body.Close()

	var geo struct {
		Results []struct {
			Lat float64 `json:"latitude"`
			Lon float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return 0, 0, fmt.Errorf("decode geo: %w", err)
	}
	if len(geo.Results) == 0 {
		return 0, 0, fmt.Errorf("city not found")
	}
	return geo.Results[0].Lat, geo.Results[0].Lon, nil
}

// --- Weather API Implementations ---
// Each API source implements the WeatherSource interface.
// Free sources: Open-Meteo
// API key required: WeatherAPI.com, Weatherstack, Meteosource, Pirate Weather, Tomorrow.io

// OpenMeteoSource fetches weather from Open-Meteo API (free, no key required).
// Uses geocoding API to convert city name to coordinates, then fetches weather.
type OpenMeteoSource struct{}

func (o *OpenMeteoSource) Name() string { return "Open-Meteo" }
func (o *OpenMeteoSource) Fetch(ctx context.Context, city string) WeatherData {
	start := time.Now()
	res := WeatherData{Source: o.Name()}

	lat, lon, err := lookupLatLon(ctx, city)
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}

	weatherURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,weather_code", lat, lon)
	resp, _, err := doGet(ctx, weatherURL)
	if err != nil {
		res.Error = fmt.Errorf("weather: %w", err)
		res.Duration = time.Since(start)
		return res
	}
	defer resp.Body.Close()

	var data struct {
		Current struct {
			Temp float64 `json:"temperature_2m"`
			Hum  float64 `json:"relative_humidity_2m"`
			Code int     `json:"weather_code"`
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		res.Error = fmt.Errorf("decode weather: %w", err)
		res.Duration = time.Since(start)
		return res
	}
	res.Temperature = data.Current.Temp
	hum := data.Current.Hum
	res.Humidity = &hum
	res.Condition = mapWMOCode(data.Current.Code)
	res.Duration = time.Since(start)
	return res
}

// mapWMOCode converts WMO weather codes to human-readable condition strings using range-based mapping.
func mapWMOCode(code int) string {
	for _, r := range WeatherCodes.WMO.Ranges {
		if code >= r.Min && code <= r.Max {
			return r.Condition
		}
	}
	return "Unknown"
}

// mapTomorrowCode converts Tomorrow.io weather codes to human-readable condition strings.
func mapTomorrowCode(code int) string {
	if condition := WeatherCodes.TomorrowIO[fmt.Sprintf("%d", code)]; condition != "" {
		return condition
	}
	return "Unknown"
}

// TomorrowIOSource fetches weather from Tomorrow.io API (requires API key).
// Returns data in JSON format with temperature, humidity, and weather description.
type TomorrowIOSource struct {
	apiKey string
}

func (t *TomorrowIOSource) Name() string { return "Tomorrow.io" }
func (t *TomorrowIOSource) Fetch(ctx context.Context, city string) WeatherData {
	start := time.Now()
	res := WeatherData{Source: t.Name()}

	lat, lon, err := lookupLatLon(ctx, city)
	if err != nil {
		res.Error = fmt.Errorf("geocoding: %w", err)
		res.Duration = time.Since(start)
		return res
	}

	url := fmt.Sprintf("https://api.tomorrow.io/v4/weather/realtime?location=%.4f,%.4f&apikey=%s", lat, lon, t.apiKey)
	resp, _, err := doGet(ctx, url)
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	defer resp.Body.Close()

	var data struct {
		Data struct {
			Values struct {
				Temp      float64 `json:"temperature"`
				Hum       float64 `json:"humidity"`
				WeatherCd int     `json:"weatherCode"`
			} `json:"values"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		res.Error = fmt.Errorf("decode: %w", err)
		res.Duration = time.Since(start)
		return res
	}

	res.Temperature = data.Data.Values.Temp
	hum := data.Data.Values.Hum
	res.Humidity = &hum
	res.Condition = mapTomorrowCode(data.Data.Values.WeatherCd)
	res.Duration = time.Since(start)
	return res
}

// WeatherAPISource fetches weather from WeatherAPI.com (requires API key).
// Provides current temperature, humidity, and detailed weather condition text.
type WeatherAPISource struct{ key string }

func (w *WeatherAPISource) Name() string { return "WeatherAPI.com" }
func (w *WeatherAPISource) Fetch(ctx context.Context, city string) WeatherData {
	start := time.Now()
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("https://api.weatherapi.com/v1/current.json?key=%s&q=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	defer resp.Body.Close()
	var data struct {
		Current struct {
			TempC float64 `json:"temp_c"`
			Hum   float64 `json:"humidity"`
			Cond  struct {
				Text string `json:"text"`
			} `json:"condition"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	res.Temperature = data.Current.TempC
	hum := data.Current.Hum
	res.Humidity = &hum
	res.Condition = data.Current.Cond.Text
	res.Duration = time.Since(start)
	return res
}

// WeatherstackSource fetches weather from Weatherstack API (requires API key).
// Note: Free tier uses HTTP (not HTTPS).
type WeatherstackSource struct{ key string }

func (w *WeatherstackSource) Name() string { return "Weatherstack" }
func (w *WeatherstackSource) Fetch(ctx context.Context, city string) WeatherData {
	start := time.Now()
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("http://api.weatherstack.com/current?access_key=%s&query=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	defer resp.Body.Close()
	var data struct {
		Current struct {
			Temp float64  `json:"temperature"`
			Hum  int      `json:"humidity"`
			Desc []string `json:"weather_descriptions"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	res.Temperature = data.Current.Temp
	hum := float64(data.Current.Hum)
	res.Humidity = &hum
	if len(data.Current.Desc) > 0 {
		res.Condition = data.Current.Desc[0]
	}
	res.Duration = time.Since(start)
	return res
}

// MeteosourceSource fetches weather from Meteosource API (requires API key).
// Free tier may not include humidity data in all responses.
type MeteosourceSource struct{ key string }

func (m *MeteosourceSource) Name() string { return "Meteosource" }
func (m *MeteosourceSource) Fetch(ctx context.Context, city string) WeatherData {
	start := time.Now()
	res := WeatherData{Source: m.Name()}
	if m.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("https://www.meteosource.com/api/v1/free/point?place_id=%s&sections=current&language=en&units=metric&key=%s", url.QueryEscape(city), m.key))
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	defer resp.Body.Close()
	var data struct {
		Current struct {
			Temp    float64     `json:"temperature"`
			Hum     interface{} `json:"humidity"`
			Summary string      `json:"summary"`
		} `json:"current"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	res.Temperature, res.Condition = data.Current.Temp, data.Current.Summary
	if h, ok := data.Current.Hum.(float64); ok {
		res.Humidity = &h
	} else if s, ok := data.Current.Hum.(string); ok {
		var hum float64
		fmt.Sscanf(strings.TrimSuffix(s, "%"), "%f", &hum)
		res.Humidity = &hum
	}
	res.Duration = time.Since(start)
	return res
}

// PirateWeatherSource fetches weather from Pirate Weather API (requires API key).
// Compatible with Dark Sky API format, uses geocoding for city lookup.
type PirateWeatherSource struct{ key string }

func (p *PirateWeatherSource) Name() string { return "Pirate Weather" }

func (p *PirateWeatherSource) Fetch(ctx context.Context, city string) WeatherData {
	start := time.Now()
	res := WeatherData{Source: p.Name()}
	if p.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	lat, lon, err := lookupLatLon(ctx, city)
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("https://api.pirateweather.net/forecast/%s/%.4f,%.4f?units=si", p.key, lat, lon))
	if err != nil {
		res.Error = fmt.Errorf("weather: %w", err)
		res.Duration = time.Since(start)
		return res
	}
	defer resp.Body.Close()
	var data struct {
		Currently struct {
			Temp float64 `json:"temperature"`
			Hum  float64 `json:"humidity"`
			Sum  string  `json:"summary"`
		} `json:"currently"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		res.Error = fmt.Errorf("decode weather: %w", err)
		res.Duration = time.Since(start)
		return res
	}
	res.Temperature = data.Currently.Temp
	hum := data.Currently.Hum * 100
	res.Humidity = &hum
	res.Condition = data.Currently.Sum
	res.Duration = time.Since(start)
	return res
}

// ========== Aggregation Functions ==========

// AggregateWeather calculates average temperature, humidity, and consensus condition.
// Only processes WeatherData entries with no errors (valid == number of successful responses).
// Returns averaged values and the most common normalized weather condition.
// Humidity average only includes sources that provided humidity data (non-nil pointers).
func AggregateWeather(data []WeatherData) (avgTemp, avgHum float64, cond string, valid int) {
	if len(data) == 0 {
		return 0, 0, "No data", 0
	}

	var tempSum, humSum float64
	var humCount int
	condCount := make(map[string]int)

	for _, d := range data {
		if d.Error == nil {
			tempSum += d.Temperature
			if d.Humidity != nil {
				humSum += *d.Humidity
				humCount++
			}
			condCount[normalizeCondition(d.Condition)]++
			valid++
		}
	}

	if valid == 0 {
		return 0, 0, "No valid data", 0
	}

	avgTemp = tempSum / float64(valid)
	if humCount > 0 {
		avgHum = humSum / float64(humCount)
	}

	maxCount := 0
	for c, count := range condCount {
		if count > maxCount {
			maxCount, cond = count, c
		}
	}
	return
}

// ConditionInfo maps normalized conditions to keywords and emoji for consistent handling.
var ConditionInfo = map[string]struct{ keywords []string; emoji string }{
	"Clear":         {[]string{"clear", "sunny"}, "☀️"},
	"Partly Cloudy": {[]string{"partly"}, "⛅"},
	"Cloudy":        {[]string{"cloud", "overcast"}, "☁️"},
	"Rainy":         {[]string{"rain", "drizzle"}, "🌧️"},
	"Snowy":         {[]string{"snow", "sleet"}, "❄️"},
	"Foggy":         {[]string{"fog", "mist"}, "🌫️"},
	"Stormy":        {[]string{"storm", "thunder"}, "⛈️"},
}

// normalizeCondition converts various weather condition strings to standard categories.
// Helps aggregate similar conditions from different APIs (e.g., "Sunny" and "Clear" → "Clear").
// Uses a map-based approach for consistency with Python implementation.
func normalizeCondition(c string) string {
	lower := strings.ToLower(c)
	for normalized, info := range ConditionInfo {
		for _, keyword := range info.keywords {
			if strings.Contains(lower, keyword) {
				return normalized
			}
		}
	}
	return c
}

// GetConditionEmoji maps weather conditions to appropriate emoji for better visualization.
// Uses the shared ConditionInfo map for consistency. Returns a default thermometer emoji
// if the condition doesn't match any known categories.
func GetConditionEmoji(c string) string {
	lower := strings.ToLower(c)
	for _, info := range ConditionInfo {
		for _, keyword := range info.keywords {
			if strings.Contains(lower, keyword) {
				return info.emoji
			}
		}
	}
	return "🌡️"
}
