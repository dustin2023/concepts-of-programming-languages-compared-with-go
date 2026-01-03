package main

import (
	"context"
	"testing"
)

func init() {
	// Load weather codes for tests
	if err := loadWeatherCodes(); err != nil {
		panic(err)
	}
}

func floatPtr(f float64) *float64 { return &f }

// TestAggregateWeather tests aggregation with valid/error data.
func TestAggregateWeather(t *testing.T) {
	tests := []struct {
		name      string
		data      []WeatherData
		wantValid int
		wantCond  string
	}{
		{"all valid", []WeatherData{
			{Source: "A", Temperature: 15, Humidity: floatPtr(60), Condition: "Cloudy"},
			{Source: "B", Temperature: 17, Humidity: floatPtr(70), Condition: "Cloudy"},
		}, 2, "Cloudy"},
		{"mixed errors", []WeatherData{
			{Source: "A", Temperature: 20, Humidity: floatPtr(50), Condition: "Clear"},
			{Source: "B", Error: &testError{}},
		}, 1, "Clear"},
		{"all errors", []WeatherData{
			{Source: "A", Error: &testError{}},
			{Source: "B", Error: &testError{}},
		}, 0, "No valid data"},
		{"empty", []WeatherData{}, 0, "No data"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, cond, valid := AggregateWeather(tt.data)
			if valid != tt.wantValid {
				t.Errorf("valid = %d, want %d", valid, tt.wantValid)
			}
			if cond != tt.wantCond {
				t.Errorf("condition = %q, want %q", cond, tt.wantCond)
			}
		})
	}
}

// TestNormalizeCondition tests weather normalization.
func TestNormalizeCondition(t *testing.T) {
	tests := []struct{ input, want string }{
		{"Clear sky", "Clear"},
		{"Partly cloudy", "Partly Cloudy"},
		{"Light rain", "Rainy"},
		{"Snow", "Snowy"},
		{"Thunderstorm", "Stormy"},
	}

	for _, tt := range tests {
		if got := normalizeCondition(tt.input); got != tt.want {
			t.Errorf("normalizeCondition(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestFetchWeatherConcurrently tests concurrent execution.
func TestFetchWeatherConcurrently(t *testing.T) {
	sources := []WeatherSource{
		&mockSource{"Mock1", 15, 60, "Clear", false},
		&mockSource{"Mock2", 16, 65, "Cloudy", false},
		&mockSource{"Mock3", 0, 0, "", true},
	}

	results := fetchWeatherConcurrently(context.Background(), "Test", sources)
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	validCount := 0
	for _, r := range results {
		if r.Error == nil {
			validCount++
		}
	}
	if validCount != 2 {
		t.Errorf("got %d valid results, want 2", validCount)
	}
}

// TestGetConditionEmoji tests emoji mapping.
func TestGetConditionEmoji(t *testing.T) {
	tests := []struct{ condition, emoji string }{
		{"Clear", "☀️"},
		{"Partly Cloudy", "⛅"},
		{"Rainy", "🌧️"},
		{"Unknown", "🌡️"},
	}

	for _, tt := range tests {
		if got := GetConditionEmoji(tt.condition); got != tt.emoji {
			t.Errorf("GetConditionEmoji(%q) = %q, want %q", tt.condition, got, tt.emoji)
		}
	}
}

// TestMapWMOCode tests WMO code mapping.
func TestMapWMOCode(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{0, "Clear"},
		{2, "Partly Cloudy"},
		{45, "Foggy"},
		{61, "Rainy"},
		{71, "Snowy"},
		{95, "Stormy"},
		{999, "Unknown"},
	}

	for _, tt := range tests {
		if got := mapWMOCode(tt.code); got != tt.want {
			t.Errorf("mapWMOCode(%d) = %q, want %q", tt.code, got, tt.want)
		}
	}
}

// Mock implementations
type mockSource struct {
	name   string
	temp   float64
	hum    float64
	cond   string
	hasErr bool
}

func (m *mockSource) Name() string { return m.name }

func (m *mockSource) Fetch(ctx context.Context, city string) WeatherData {
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

