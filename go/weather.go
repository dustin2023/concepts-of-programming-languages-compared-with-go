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
	"sync"
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
	Conditions map[string]struct {
		Keywords []string `json:"keywords"`
		Emoji    string   `json:"emoji"`
	} `json:"conditions"`
}

// WeatherCodes holds the unified weather code mappings loaded from JSON.
var WeatherCodes WeatherCodeConfig
var weatherCodesOnce sync.Once
var weatherCodesErr error


// WeatherData represents weather from a single source.
// Temperature in Celsius, Humidity as percentage (0-100).
type WeatherData struct {
	Source      string
	Temperature float64
	Humidity    *float64 // Pointer to distinguish between 0% and missing data
	Condition   string
	Error       error
	Duration    time.Duration
}

// WeatherSource interface - each source implements Fetch().
type WeatherSource interface {
	Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData
	Name() string
}


// client is a shared HTTP client with 10s timeout.
var client = &http.Client{
	Timeout: 10 * time.Second,
}

// loadWeatherCodes loads weather code mappings from shared JSON file.
// Uses sync.Once to ensure thread-safe single initialization.
func loadWeatherCodes() error {
	weatherCodesOnce.Do(func() {
		path := filepath.Join("..", "weather_codes.json")
		data, err := os.ReadFile(path)
		if err != nil {
			weatherCodesErr = fmt.Errorf("failed to read weather_codes.json: %w", err)
			return
		}
		if err := json.Unmarshal(data, &WeatherCodes); err != nil {
			weatherCodesErr = fmt.Errorf("failed to parse weather_codes.json: %w", err)
			return
		}
	})
	return weatherCodesErr
}


// doGet creates request with context and returns response + duration.
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

// getCoordinates gets coordinates from cache or performs geocoding.
func getCoordinates(ctx context.Context, city string, coordsCache map[string][2]float64) (float64, float64, error) {
	if coordsCache != nil {
		if coords, ok := coordsCache[city]; ok {
			return coords[0], coords[1], nil
		}
	}
	return lookupLatLon(ctx, city)
}

// lookupLatLon resolves a city name to coordinates using Open-Meteo geocoding.
func lookupLatLon(ctx context.Context, city string) (float64, float64, error) {
	geoURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1", url.QueryEscape(city))
	resp, _, err := doGet(ctx, geoURL)
	if err != nil {
		return 0, 0, fmt.Errorf("geocoding request failed: %w", err)
	}
	defer resp.Body.Close()

	var geo struct {
		Results []struct {
			Lat float64 `json:"latitude"`
			Lon float64 `json:"longitude"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&geo); err != nil {
		return 0, 0, fmt.Errorf("failed to decode geocoding response: %w", err)
	}
	if len(geo.Results) == 0 {
		return 0, 0, fmt.Errorf("city %q not found", city)
	}
	return geo.Results[0].Lat, geo.Results[0].Lon, nil
}

// --- Weather API Implementations ---
// Each API source implements the WeatherSource interface.
// Free sources: Open-Meteo
// API key required: WeatherAPI.com, Weatherstack, Meteosource, Pirate Weather, Tomorrow.io

// OpenMeteoSource - free API, no key required.
type OpenMeteoSource struct{}

func (o *OpenMeteoSource) Name() string { return "Open-Meteo" }
func (o *OpenMeteoSource) Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData {
	start := time.Now()
	res := WeatherData{Source: o.Name()}

	// Ensure weather codes are loaded
	if err := loadWeatherCodes(); err != nil {
		res.Error = fmt.Errorf("configuration error: %w", err)
		res.Duration = time.Since(start)
		return res
	}

	lat, lon, err := getCoordinates(ctx, city, coordsCache)
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}

	weatherURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,weather_code", lat, lon)
	resp, _, err := doGet(ctx, weatherURL)
	if err != nil {
		res.Error = fmt.Errorf("weather request failed: %w", err)
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
		res.Error = fmt.Errorf("failed to decode weather response: %w", err)
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

// TomorrowIOSource - requires API key, coordinate-based.
type TomorrowIOSource struct {
	apiKey string
}

func (t *TomorrowIOSource) Name() string { return "Tomorrow.io" }
func (t *TomorrowIOSource) Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData {
	start := time.Now()
	res := WeatherData{Source: t.Name()}

	if t.apiKey == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}

	lat, lon, err := getCoordinates(ctx, city, coordsCache)
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}

	url := fmt.Sprintf("https://api.tomorrow.io/v4/weather/realtime?location=%.4f,%.4f&apikey=%s", lat, lon, t.apiKey)
	resp, _, err := doGet(ctx, url)
	if err != nil {
		res.Error = fmt.Errorf("weather request failed: %w", err)
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
		res.Error = fmt.Errorf("failed to decode response: %w", err)
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

// WeatherAPISource - requires API key.
type WeatherAPISource struct{ key string }

func (w *WeatherAPISource) Name() string { return "WeatherAPI.com" }
func (w *WeatherAPISource) Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData {
	start := time.Now()
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("https://api.weatherapi.com/v1/current.json?key=%s&q=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = fmt.Errorf("weather request failed: %w", err)
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
		res.Error = fmt.Errorf("failed to decode response: %w", err)
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

// WeatherstackSource - requires API key, HTTP only on free tier.
type WeatherstackSource struct{ key string }

func (w *WeatherstackSource) Name() string { return "Weatherstack" }
func (w *WeatherstackSource) Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData {
	start := time.Now()
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("http://api.weatherstack.com/current?access_key=%s&query=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = fmt.Errorf("weather request failed: %w", err)
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
		res.Error = fmt.Errorf("failed to decode response: %w", err)
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

// MeteosourceSource - requires API key, may lack humidity on free tier.
type MeteosourceSource struct{ key string }

func (m *MeteosourceSource) Name() string { return "Meteosource" }
func (m *MeteosourceSource) Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData {
	start := time.Now()
	res := WeatherData{Source: m.Name()}
	if m.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	lat, lon, err := getCoordinates(ctx, city, coordsCache)
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("https://www.meteosource.com/api/v1/free/point?lat=%.4f&lon=%.4f&sections=current&language=en&units=metric&key=%s", lat, lon, m.key))
	if err != nil {
		res.Error = fmt.Errorf("weather request failed: %w", err)
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
		res.Error = fmt.Errorf("failed to decode response: %w", err)
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

// PirateWeatherSource - Dark Sky compatible, requires API key.
type PirateWeatherSource struct{ key string }

func (p *PirateWeatherSource) Name() string { return "Pirate Weather" }

func (p *PirateWeatherSource) Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData {
	start := time.Now()
	res := WeatherData{Source: p.Name()}
	if p.key == "" {
		res.Error = fmt.Errorf("API key required")
		res.Duration = time.Since(start)
		return res
	}
	lat, lon, err := getCoordinates(ctx, city, coordsCache)
	if err != nil {
		res.Error = err
		res.Duration = time.Since(start)
		return res
	}
	resp, _, err := doGet(ctx, fmt.Sprintf("https://api.pirateweather.net/forecast/%s/%.4f,%.4f?units=si", p.key, lat, lon))
	if err != nil {
		res.Error = fmt.Errorf("weather request failed: %w", err)
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
		res.Error = fmt.Errorf("failed to decode response: %w", err)
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

// fetchWeatherConcurrently fetches from all sources in parallel using goroutines.
// Pre-geocodes the city to reduce redundant API calls.
func fetchWeatherConcurrently(ctx context.Context, city string, sources []WeatherSource) []WeatherData {
	// Pre-geocode city once to avoid redundant calls from each source
	coordsCache := make(map[string][2]float64)
	if lat, lon, err := lookupLatLon(ctx, city); err == nil {
		coordsCache[city] = [2]float64{lat, lon}
	}

	ch := make(chan WeatherData, len(sources))
	for _, s := range sources {
		go func(src WeatherSource) { ch <- src.Fetch(ctx, city, coordsCache) }(s)
	}
	results := make([]WeatherData, 0, len(sources))
	for i := 0; i < len(sources); i++ {
		results = append(results, <-ch)
	}
	return results
}

// fetchSequential fetches weather data sequentially for performance comparison.
func fetchSequential(ctx context.Context, city string, sources []WeatherSource) []WeatherData {
	// Pre-geocode city once to avoid redundant calls
	coordsCache := make(map[string][2]float64)
	if lat, lon, err := lookupLatLon(ctx, city); err == nil {
		coordsCache[city] = [2]float64{lat, lon}
	}

	results := make([]WeatherData, 0, len(sources))
	for _, s := range sources {
		results = append(results, s.Fetch(ctx, city, coordsCache))
	}
	return results
}

// AggregateWeather calculates avg temp/humidity and consensus condition from valid data.
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


// mapWMOCode converts WMO codes to readable conditions.
func mapWMOCode(code int) string {
	for _, r := range WeatherCodes.WMO.Ranges {
		if code >= r.Min && code <= r.Max {
			return r.Condition
		}
	}
	return "Unknown"
}

// mapTomorrowCode converts Tomorrow.io codes to readable conditions.
func mapTomorrowCode(code int) string {
	if condition := WeatherCodes.TomorrowIO[fmt.Sprintf("%d", code)]; condition != "" {
		return condition
	}
	return "Unknown"
}

// normalizeCondition converts conditions to standard categories.
// Checks more specific patterns first (e.g., "Partly Cloudy" before "Cloudy").
func normalizeCondition(c string) string {
	lower := strings.ToLower(c)
	
	// Check in priority order (most specific first)
	conditionOrder := []string{"Partly Cloudy", "Clear", "Cloudy", "Rainy", "Snowy", "Foggy", "Stormy"}
	
	for _, normalized := range conditionOrder {
		if info, exists := WeatherCodes.Conditions[normalized]; exists {
			for _, keyword := range info.Keywords {
				if strings.Contains(lower, keyword) {
					return normalized
				}
			}
		}
	}
	return c
}

// GetConditionEmoji maps conditions to emoji. Returns thermometer if no match.
func GetConditionEmoji(c string) string {
	lower := strings.ToLower(c)
	for _, info := range WeatherCodes.Conditions {
		for _, keyword := range info.Keywords {
			if strings.Contains(lower, keyword) {
				return info.Emoji
			}
		}
	}
	return "🌡️"
}
