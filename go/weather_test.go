package main

import (
	"context"
	"testing"
	"time"
)

func init() {
	if err := loadWeatherCodes(); err != nil {
		panic(err)
	}
}

func TestValidateCityName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantCity  string
		wantError bool
	}{
		{"valid single", "Munich", "Munich", false},
		{"valid multi-word", "New York", "New York", false},
		{"valid Unicode", "São Paulo", "São Paulo", false},
		{"valid with apostrophe", "O'Brien", "O'Brien", false},
		{"empty string", "", "", true},
		{"only whitespace", "   ", "", true},
		{"flag-like", "--city", "", true},
		{"dash prefix", "-Munich", "", true},
		{"exceeds max length", "A" + string(make([]byte, 100)), "", true},
		{"invalid char @", "City@Name", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateCityName(tt.input)
			if (err != nil) != tt.wantError {
				t.Errorf("error = %v, wantErr = %v", err, tt.wantError)
			}
			if got != tt.wantCity {
				t.Errorf("got %q, want %q", got, tt.wantCity)
			}
		})
	}
}

func TestNormalizeSourceName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Open-Meteo", "openmeteo"},
		{"Pirate Weather", "pirateweather"},
		{"WeatherAPI.com", "weatherapicom"},
		{"TOMORROW.IO", "tomorrowio"},
	}

	for _, tt := range tests {
		if got := normalizeSourceName(tt.input); got != tt.expected {
			t.Errorf("normalizeSourceName(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func floatPtr(f float64) *float64 { return &f }

func TestAggregateWeather(t *testing.T) {
	tests := []struct {
		name      string
		data      []WeatherData
		wantValid int
		wantTemp  float64
		wantHum   float64
		wantCond  string
	}{
		{
			"all valid",
			[]WeatherData{
				{Source: "A", Temperature: 10, Humidity: floatPtr(50), Condition: "Cloudy"},
				{Source: "B", Temperature: 20, Humidity: floatPtr(70), Condition: "Cloudy"},
			},
			2, 15.0, 60.0, "Cloudy",
		},
		{
			"mixed with errors",
			[]WeatherData{
				{Source: "A", Temperature: 20, Humidity: floatPtr(50), Condition: "Clear"},
				{Source: "B", Error: &testError{}},
			},
			1, 20.0, 50.0, "Clear",
		},
		{
			"missing humidity",
			[]WeatherData{
				{Source: "A", Temperature: 15, Humidity: floatPtr(60), Condition: "Rainy"},
				{Source: "B", Temperature: 15, Humidity: nil, Condition: "Rainy"},
			},
			2, 15.0, 60.0, "Rainy",
		},
		{
			"all errors",
			[]WeatherData{
				{Source: "A", Error: &testError{}},
				{Source: "B", Error: &testError{}},
			},
			0, 0.0, 0.0, "No valid data",
		},
		{
			"empty",
			[]WeatherData{},
			0, 0.0, 0.0, "No data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			avgTemp, avgHum, cond, valid := AggregateWeather(tt.data)

			if valid != tt.wantValid {
				t.Errorf("valid = %d, want %d", valid, tt.wantValid)
			}
			if valid > 0 && avgTemp != tt.wantTemp {
				t.Errorf("temp = %.1f, want %.1f", avgTemp, tt.wantTemp)
			}
			if valid > 0 && avgHum != tt.wantHum {
				t.Errorf("hum = %.1f, want %.1f", avgHum, tt.wantHum)
			}
			if cond != tt.wantCond {
				t.Errorf("cond = %q, want %q", cond, tt.wantCond)
			}
		})
	}
}

type mockSource struct {
	name   string
	temp   float64
	hum    float64
	cond   string
	hasErr bool
}

func (m *mockSource) Name() string { return m.name }

func (m *mockSource) Fetch(ctx context.Context, city string, coordsCache map[string][2]float64) WeatherData {
	if m.hasErr {
		return WeatherData{Source: m.name, Error: &testError{}}
	}
	return WeatherData{
		Source:      m.name,
		Temperature: m.temp,
		Humidity:    floatPtr(m.hum),
		Condition:   m.cond,
	}
}

type testError struct{}

func (e *testError) Error() string { return "test error" }

func TestFetchWeatherBehavior(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	sources := []WeatherSource{
		&mockSource{name: "S1", temp: 15, hum: 60, cond: "Clear", hasErr: false},
		&mockSource{name: "S2", temp: 16, hum: 65, cond: "Cloudy", hasErr: false},
		&mockSource{name: "S3", temp: 0, hum: 0, cond: "", hasErr: true},
	}

	t.Run("concurrent", func(t *testing.T) {
		results := fetchWeatherConcurrently(ctx, "TestCity", sources)
		if len(results) != 3 {
			t.Errorf("got %d results, want 3", len(results))
		}
		validCount := 0
		for _, r := range results {
			if r.Error == nil {
				validCount++
			}
		}
		if validCount != 2 {
			t.Errorf("valid = %d, want 2", validCount)
		}
	})

	t.Run("sequential", func(t *testing.T) {
		results := fetchSequential(ctx, "TestCity", sources)
		if len(results) != 3 {
			t.Errorf("got %d results, want 3", len(results))
		}
		if results[0].Source != "S1" || results[1].Source != "S2" {
			t.Errorf("order lost")
		}
	})
}

