package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// WeatherData represents the weather information from a single source.
// Temperature is in Celsius, Humidity is a percentage (0-100).
// Error field contains any error that occurred during fetching.
type WeatherData struct {
	Source      string
	Temperature float64
	Humidity    float64
	Condition   string
	Error       error
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
func doGet(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "weather-aggregator/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, resp.Status)
	}
	return resp, nil
}

// lookupLatLon resolves a city name to coordinates using Open-Meteo geocoding.
func lookupLatLon(ctx context.Context, city string) (float64, float64, error) {
	geoURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1", url.QueryEscape(city))
	resp, err := doGet(ctx, geoURL)
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
// Free sources: Open-Meteo, wttr.in
// API key required: WeatherAPI.com, Weatherstack, Meteosource, Pirate Weather

// OpenMeteoSource fetches weather from Open-Meteo API (free, no key required).
// Uses geocoding API to convert city name to coordinates, then fetches weather.
type OpenMeteoSource struct{}

func (o *OpenMeteoSource) Name() string { return "Open-Meteo" }
func (o *OpenMeteoSource) Fetch(ctx context.Context, city string) WeatherData {
	res := WeatherData{Source: o.Name()}

	lat, lon, err := lookupLatLon(ctx, city)
	if err != nil {
		res.Error = err
		return res
	}

	weatherURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,weather_code", lat, lon)
	resp, err := doGet(ctx, weatherURL)
	if err != nil {
		res.Error = fmt.Errorf("weather: %w", err)
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
		return res
	}
	res.Temperature, res.Humidity, res.Condition = data.Current.Temp, data.Current.Hum, mapWMOCode(data.Current.Code)
	return res
}

// mapWMOCode converts WMO weather codes to human-readable condition strings.
// WMO codes: 0=Clear, 1-3=Cloudy, 45-48=Fog, 51-67=Rain, 71-86=Snow, 95+=Storms
func mapWMOCode(code int) string {
	switch {
	case code == 0:
		return "Clear"
	case code <= 3:
		return "Partly Cloudy"
	case code <= 48:
		return "Foggy"
	case code <= 67:
		return "Rainy"
	case code <= 86:
		return "Snowy"
	default:
		return "Stormy"
	}
}

// WttrinSource fetches weather from wttr.in API (free, no key required).
// Returns data in JSON format with temperature, humidity, and weather description.
type WttrinSource struct{}

func (w *WttrinSource) Name() string { return "wttr.in" }
func (w *WttrinSource) Fetch(ctx context.Context, city string) WeatherData {
	res := WeatherData{Source: w.Name()}
	resp, err := doGet(ctx, "https://wttr.in/"+url.QueryEscape(city)+"?format=j1")
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()

	var data struct {
		Current []struct {
			TempC string `json:"temp_C"`
			Hum   string `json:"humidity"`
			Desc  []struct {
				Val string `json:"value"`
			} `json:"weatherDesc"`
		} `json:"current_condition"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		res.Error = fmt.Errorf("decode: %w", err)
		return res
	}
	if len(data.Current) == 0 {
		res.Error = fmt.Errorf("no current weather data")
		return res
	}

	temp, err := strconv.ParseFloat(strings.TrimSpace(data.Current[0].TempC), 64)
	if err != nil {
		res.Error = fmt.Errorf("parse temperature: %w", err)
		return res
	}
	hum, err := strconv.ParseFloat(strings.TrimSpace(data.Current[0].Hum), 64)
	if err != nil {
		res.Error = fmt.Errorf("parse humidity: %w", err)
		return res
	}
	res.Temperature = temp
	res.Humidity = hum
	if len(data.Current[0].Desc) > 0 {
		res.Condition = data.Current[0].Desc[0].Val
	}
	return res
}

// WeatherAPISource fetches weather from WeatherAPI.com (requires API key).
// Provides current temperature, humidity, and detailed weather condition text.
type WeatherAPISource struct{ key string }

func (w *WeatherAPISource) Name() string { return "WeatherAPI.com" }
func (w *WeatherAPISource) Fetch(ctx context.Context, city string) WeatherData {
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := doGet(ctx, fmt.Sprintf("https://api.weatherapi.com/v1/current.json?key=%s&q=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = err
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
		return res
	}
	res.Temperature, res.Humidity, res.Condition = data.Current.TempC, data.Current.Hum, data.Current.Cond.Text
	return res
}

// WeatherstackSource fetches weather from Weatherstack API (requires API key).
// Note: Free tier uses HTTP (not HTTPS).
type WeatherstackSource struct{ key string }

func (w *WeatherstackSource) Name() string { return "Weatherstack" }
func (w *WeatherstackSource) Fetch(ctx context.Context, city string) WeatherData {
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := doGet(ctx, fmt.Sprintf("http://api.weatherstack.com/current?access_key=%s&query=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = err
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
		return res
	}
	res.Temperature, res.Humidity = data.Current.Temp, float64(data.Current.Hum)
	if len(data.Current.Desc) > 0 {
		res.Condition = data.Current.Desc[0]
	}
	return res
}

// MeteosourceSource fetches weather from Meteosource API (requires API key).
// Free tier may not include humidity data in all responses.
type MeteosourceSource struct{ key string }

func (m *MeteosourceSource) Name() string { return "Meteosource" }
func (m *MeteosourceSource) Fetch(ctx context.Context, city string) WeatherData {
	res := WeatherData{Source: m.Name()}
	if m.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := doGet(ctx, fmt.Sprintf("https://www.meteosource.com/api/v1/free/point?place_id=%s&sections=current&language=en&units=metric&key=%s", url.QueryEscape(city), m.key))
	if err != nil {
		res.Error = err
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
		return res
	}
	res.Temperature, res.Condition = data.Current.Temp, data.Current.Summary
	if h, ok := data.Current.Hum.(float64); ok {
		res.Humidity = h
	} else if s, ok := data.Current.Hum.(string); ok {
		fmt.Sscanf(strings.TrimSuffix(s, "%"), "%f", &res.Humidity)
	}
	return res
}

// PirateWeatherSource fetches weather from Pirate Weather API (requires API key).
// Compatible with Dark Sky API format, uses geocoding for city lookup.
type PirateWeatherSource struct{ key string }

func (p *PirateWeatherSource) Name() string { return "Pirate Weather" }

func (p *PirateWeatherSource) Fetch(ctx context.Context, city string) WeatherData {
	res := WeatherData{Source: p.Name()}
	if p.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	lat, lon, err := lookupLatLon(ctx, city)
	if err != nil {
		res.Error = err
		return res
	}
	resp, err := doGet(ctx, fmt.Sprintf("https://api.pirateweather.net/forecast/%s/%.4f,%.4f?units=si", p.key, lat, lon))
	if err != nil {
		res.Error = fmt.Errorf("weather: %w", err)
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
		return res
	}
	res.Temperature, res.Humidity, res.Condition = data.Currently.Temp, data.Currently.Hum*100, data.Currently.Sum
	return res
}

// ========== Aggregation Functions ==========

// AggregateWeather calculates average temperature, humidity, and consensus condition.
// Only processes WeatherData entries with no errors (valid == number of successful responses).
// Returns averaged values and the most common normalized weather condition.
func AggregateWeather(data []WeatherData) (avgTemp, avgHum float64, cond string, valid int) {
	if len(data) == 0 {
		return 0, 0, "No data", 0
	}

	var tempSum, humSum float64
	condCount := make(map[string]int)

	for _, d := range data {
		if d.Error == nil {
			tempSum += d.Temperature
			humSum += d.Humidity
			condCount[normalizeCondition(d.Condition)]++
			valid++
		}
	}

	if valid == 0 {
		return 0, 0, "No valid data", 0
	}

	avgTemp = tempSum / float64(valid)
	avgHum = humSum / float64(valid)

	maxCount := 0
	for c, count := range condCount {
		if count > maxCount {
			maxCount, cond = count, c
		}
	}
	return
}

// normalizeCondition converts various weather condition strings to standard categories.
// Helps aggregate similar conditions from different APIs (e.g., "Sunny" and "Clear" → "Clear").
// Categories: Clear, Partly Cloudy, Cloudy, Rainy, Snowy, Stormy, Foggy.
func normalizeCondition(c string) string {
	lower := strings.ToLower(c)
	if strings.Contains(lower, "clear") || strings.Contains(lower, "sunny") {
		return "Clear"
	}
	if strings.Contains(lower, "partly") {
		return "Partly Cloudy"
	}
	if strings.Contains(lower, "cloud") || strings.Contains(lower, "overcast") {
		return "Cloudy"
	}
	if strings.Contains(lower, "rain") || strings.Contains(lower, "drizzle") {
		return "Rainy"
	}
	if strings.Contains(lower, "snow") || strings.Contains(lower, "sleet") {
		return "Snowy"
	}
	if strings.Contains(lower, "fog") || strings.Contains(lower, "mist") {
		return "Foggy"
	}
	if strings.Contains(lower, "storm") || strings.Contains(lower, "thunder") {
		return "Stormy"
	}
	return c
}

// GetConditionEmoji maps weather conditions to appropriate emoji for better visualization.
// Used in output display to make results more user-friendly.
func GetConditionEmoji(c string) string {
	lower := strings.ToLower(c)
	if strings.Contains(lower, "clear") || strings.Contains(lower, "sunny") {
		return "☀️"
	}
	if strings.Contains(lower, "partly") {
		return "⛅"
	}
	if strings.Contains(lower, "cloud") || strings.Contains(lower, "overcast") {
		return "☁️"
	}
	if strings.Contains(lower, "rain") || strings.Contains(lower, "drizzle") {
		return "🌧️"
	}
	if strings.Contains(lower, "snow") || strings.Contains(lower, "sleet") {
		return "❄️"
	}
	if strings.Contains(lower, "fog") || strings.Contains(lower, "mist") {
		return "🌫️"
	}
	if strings.Contains(lower, "storm") || strings.Contains(lower, "thunder") {
		return "⛈️"
	}
	return "🌡️"
}
