package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type WeatherData struct {
	Source      string
	Temperature float64
	Humidity    float64
	Condition   string
	Error       error
}

type WeatherSource interface {
	Fetch(city string) WeatherData
	Name() string
}

func fetchWeatherConcurrently(city string, sources []WeatherSource) []WeatherData {
	ch := make(chan WeatherData, len(sources))
	for _, s := range sources {
		go func(src WeatherSource) { ch <- src.Fetch(city) }(s)
	}
	results := make([]WeatherData, 0, len(sources))
	for i := 0; i < len(sources); i++ {
		results = append(results, <-ch)
	}
	return results
}

var client = &http.Client{Timeout: 10 * time.Second}

// Open-Meteo API
type OpenMeteoSource struct{}

func (o *OpenMeteoSource) Name() string { return "Open-Meteo" }
func (o *OpenMeteoSource) Fetch(city string) WeatherData {
	res := WeatherData{Source: o.Name()}
	geoURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1", url.QueryEscape(city))
	resp, err := client.Get(geoURL)
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("geo status %d", resp.StatusCode)
		return res
	}
	var geo struct {
		Results []struct {
			Lat float64 `json:"latitude"`
			Lon float64 `json:"longitude"`
		} `json:"results"`
	}
	if json.NewDecoder(resp.Body).Decode(&geo) != nil || len(geo.Results) == 0 {
		res.Error = fmt.Errorf("city not found")
		return res
	}
	weatherURL := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%.4f&longitude=%.4f&current=temperature_2m,relative_humidity_2m,weather_code", geo.Results[0].Lat, geo.Results[0].Lon)
	resp, err = client.Get(weatherURL)
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("weather status %d", resp.StatusCode)
		return res
	}
	var data struct {
		Current struct {
			Temp float64 `json:"temperature_2m"`
			Hum  float64 `json:"relative_humidity_2m"`
			Code int     `json:"weather_code"`
		}
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		res.Error = fmt.Errorf("decode failed")
		return res
	}
	res.Temperature, res.Humidity, res.Condition = data.Current.Temp, data.Current.Hum, mapWMOCode(data.Current.Code)
	return res
}

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

// wttr.in API
type WttrinSource struct{}

func (w *WttrinSource) Name() string { return "wttr.in" }
func (w *WttrinSource) Fetch(city string) WeatherData {
	res := WeatherData{Source: w.Name()}
	resp, err := client.Get("https://wttr.in/" + url.QueryEscape(city) + "?format=j1")
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
	if json.NewDecoder(resp.Body).Decode(&data) != nil || len(data.Current) == 0 {
		res.Error = fmt.Errorf("decode failed")
		return res
	}
	fmt.Sscanf(data.Current[0].TempC, "%f", &res.Temperature)
	fmt.Sscanf(data.Current[0].Hum, "%f", &res.Humidity)
	if len(data.Current[0].Desc) > 0 {
		res.Condition = data.Current[0].Desc[0].Val
	}
	return res
}

// OpenWeatherMap API
type OpenWeatherSource struct{ key string }

func (o *OpenWeatherSource) Name() string { return "OpenWeatherMap" }
func (o *OpenWeatherSource) Fetch(city string) WeatherData {
	res := WeatherData{Source: o.Name()}
	if o.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := client.Get(fmt.Sprintf("https://api.openweathermap.org/data/2.5/weather?q=%s&appid=%s&units=metric", url.QueryEscape(city), o.key))
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	var data struct {
		Main    struct{ Temp, Hum float64 }
		Weather []struct{ Main string }
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		res.Error = fmt.Errorf("decode failed")
		return res
	}
	res.Temperature, res.Humidity = data.Main.Temp, data.Main.Hum
	if len(data.Weather) > 0 {
		res.Condition = data.Weather[0].Main
	}
	return res
}

// WeatherAPI.com
type WeatherAPISource struct{ key string }

func (w *WeatherAPISource) Name() string { return "WeatherAPI.com" }
func (w *WeatherAPISource) Fetch(city string) WeatherData {
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := client.Get(fmt.Sprintf("https://api.weatherapi.com/v1/current.json?key=%s&q=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("status %d", resp.StatusCode)
		return res
	}
	var data struct {
		Current struct {
			TempC float64 `json:"temp_c"`
			Hum   float64 `json:"humidity"`
			Cond  struct {
				Text string `json:"text"`
			} `json:"condition"`
		} `json:"current"`
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		res.Error = fmt.Errorf("decode failed")
		return res
	}
	res.Temperature, res.Humidity, res.Condition = data.Current.TempC, data.Current.Hum, data.Current.Cond.Text
	return res
}

// Weatherstack API
type WeatherstackSource struct{ key string }

func (w *WeatherstackSource) Name() string { return "Weatherstack" }
func (w *WeatherstackSource) Fetch(city string) WeatherData {
	res := WeatherData{Source: w.Name()}
	if w.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := client.Get(fmt.Sprintf("http://api.weatherstack.com/current?access_key=%s&query=%s", w.key, url.QueryEscape(city)))
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("status %d", resp.StatusCode)
		return res
	}
	var data struct {
		Current struct {
			Temp float64  `json:"temperature"`
			Hum  int      `json:"humidity"`
			Desc []string `json:"weather_descriptions"`
		} `json:"current"`
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		res.Error = fmt.Errorf("decode failed")
		return res
	}
	res.Temperature, res.Humidity = data.Current.Temp, float64(data.Current.Hum)
	if len(data.Current.Desc) > 0 {
		res.Condition = data.Current.Desc[0]
	}
	return res
}

// Visual Crossing API
type VisualCrossingSource struct{ key string }

func (v *VisualCrossingSource) Name() string { return "Visual Crossing" }
func (v *VisualCrossingSource) Fetch(city string) WeatherData {
	res := WeatherData{Source: v.Name()}
	if v.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := client.Get(fmt.Sprintf("https://weather.visualcrossing.com/VisualCrossingWebServices/rest/services/timeline/%s?unitGroup=metric&key=%s&contentType=json&include=current", url.QueryEscape(city), v.key))
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("status %d", resp.StatusCode)
		return res
	}
	var data struct {
		CurrentConditions struct {
			Temp float64 `json:"temp"`
			Hum  float64 `json:"humidity"`
			Cond string  `json:"conditions"`
		} `json:"currentConditions"`
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		res.Error = fmt.Errorf("decode failed")
		return res
	}
	res.Temperature, res.Humidity, res.Condition = data.CurrentConditions.Temp, data.CurrentConditions.Hum, data.CurrentConditions.Cond
	return res
}

// Meteosource API
type MeteosourceSource struct{ key string }

func (m *MeteosourceSource) Name() string { return "Meteosource" }
func (m *MeteosourceSource) Fetch(city string) WeatherData {
	res := WeatherData{Source: m.Name()}
	if m.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	resp, err := client.Get(fmt.Sprintf("https://www.meteosource.com/api/v1/free/point?place_id=%s&sections=current&language=en&units=metric&key=%s", url.QueryEscape(city), m.key))
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("status %d", resp.StatusCode)
		return res
	}
	var data struct {
		Current struct {
			Temp    float64     `json:"temperature"`
			Hum     interface{} `json:"humidity"`
			Summary string      `json:"summary"`
		} `json:"current"`
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		res.Error = fmt.Errorf("decode failed")
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

// Pirate Weather API
type PirateWeatherSource struct{ key string }

func (p *PirateWeatherSource) Name() string { return "Pirate Weather" }
func (p *PirateWeatherSource) Fetch(city string) WeatherData {
	res := WeatherData{Source: p.Name()}
	if p.key == "" {
		res.Error = fmt.Errorf("API key required")
		return res
	}
	geoURL := fmt.Sprintf("https://geocoding-api.open-meteo.com/v1/search?name=%s&count=1", url.QueryEscape(city))
	resp, err := client.Get(geoURL)
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("geo status %d", resp.StatusCode)
		return res
	}
	var geo struct {
		Results []struct {
			Lat float64 `json:"latitude"`
			Lon float64 `json:"longitude"`
		} `json:"results"`
	}
	if json.NewDecoder(resp.Body).Decode(&geo) != nil || len(geo.Results) == 0 {
		res.Error = fmt.Errorf("city not found")
		return res
	}
	resp, err = client.Get(fmt.Sprintf("https://api.pirateweather.net/forecast/%s/%.4f,%.4f?units=si", p.key, geo.Results[0].Lat, geo.Results[0].Lon))
	if err != nil {
		res.Error = err
		return res
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		res.Error = fmt.Errorf("weather status %d", resp.StatusCode)
		return res
	}
	var data struct {
		Currently struct {
			Temp float64 `json:"temperature"`
			Hum  float64 `json:"humidity"`
			Sum  string  `json:"summary"`
		} `json:"currently"`
	}
	if json.NewDecoder(resp.Body).Decode(&data) != nil {
		res.Error = fmt.Errorf("decode failed")
		return res
	}
	res.Temperature, res.Humidity, res.Condition = data.Currently.Temp, data.Currently.Hum*100, data.Currently.Sum
	return res
}

// ========== Aggregation ==========
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
