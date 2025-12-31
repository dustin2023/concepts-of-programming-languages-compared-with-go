package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestAggregateWeather verifies the aggregation logic for weather data.
// Tests different scenarios: all valid, partial errors, all errors, empty input.
func TestAggregateWeather(t *testing.T) {
	tests := []struct {
		name      string
		data      []WeatherData
		wantValid int
		wantCond  string
	}{
		{"all valid", []WeatherData{
			{Source: "A", Temperature: 15, Humidity: 60, Condition: "Cloudy"},
			{Source: "B", Temperature: 16, Humidity: 65, Condition: "Cloudy"},
		}, 2, "Cloudy"},
		{"some errors", []WeatherData{
			{Source: "A", Temperature: 15, Humidity: 60, Condition: "Cloudy"},
			{Source: "B", Error: &testError{}},
		}, 1, "Cloudy"},
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

// TestNormalizeCondition verifies that different weather descriptions are correctly
// normalized to standard categories (Clear, Cloudy, Rainy, etc.).
func TestNormalizeCondition(t *testing.T) {
	tests := []struct{ input, want string }{
		{"Clear sky", "Clear"},
		{"Partly cloudy", "Partly Cloudy"},
		{"Overcast", "Cloudy"},
		{"Light rain", "Rainy"},
		{"Snow", "Snowy"},
		{"Fog", "Foggy"},
		{"Thunderstorm", "Stormy"},
	}

	for _, tt := range tests {
		if got := normalizeCondition(tt.input); got != tt.want {
			t.Errorf("normalizeCondition(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestFetchWeatherConcurrently verifies that concurrent fetching works correctly.
// Uses mock sources to test goroutine-based concurrent execution.
func TestFetchWeatherConcurrently(t *testing.T) {
	sources := []WeatherSource{
		&mockSource{"Mock1", 15, 60, "Clear", false},
		&mockSource{"Mock2", 16, 65, "Cloudy", false},
		&mockSource{"Mock3", 0, 0, "", true},
	}

	ctx := context.Background()
	results := fetchWeatherConcurrently(ctx, "Test", sources)

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

// BenchmarkFetchWeatherConcurrently measures performance of concurrent fetching.
// Useful for comparing concurrent vs sequential execution speeds.
func BenchmarkFetchWeatherConcurrently(b *testing.B) {
	sources := []WeatherSource{
		&mockSource{"M1", 15, 60, "Clear", false},
		&mockSource{"M2", 16, 65, "Cloudy", false},
	}

	ctx := context.Background()
	for i := 0; i < b.N; i++ {
		fetchWeatherConcurrently(ctx, "Test", sources)
	}
}

// BenchmarkAggregateWeather measures performance of weather data aggregation.
func BenchmarkAggregateWeather(b *testing.B) {
	data := []WeatherData{
		{Source: "A", Temperature: 15, Humidity: 60, Condition: "Cloudy"},
		{Source: "B", Temperature: 16, Humidity: 65, Condition: "Cloudy"},
	}

	for i := 0; i < b.N; i++ {
		AggregateWeather(data)
	}
}

// mockSource is a test double that implements WeatherSource interface.
// Used to test concurrent fetching without making real API calls.
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
		Humidity:    m.hum,
		Condition:   m.cond,
	}
}

// testError is a simple error type for testing error handling.
type testError struct{}

func (e *testError) Error() string { return "test error" }

// mockSlowSource simulates a slow source that returns only after the given delay
// or returns the context error if the context is cancelled earlier.
type mockSlowSource struct {
	name  string
	delay time.Duration
}

func (m *mockSlowSource) Name() string { return m.name }

func (m *mockSlowSource) Fetch(ctx context.Context, city string) WeatherData {
	select {
	case <-ctx.Done():
		return WeatherData{Source: m.name, Error: ctx.Err()}
	case <-time.After(m.delay):
		return WeatherData{Source: m.name, Temperature: 1.0, Humidity: 1.0, Condition: "OK"}
	}
}

// TestContextCancellation verifies that a slow source returns a context error
// when the overall context deadline expires.
func TestContextCancellation(t *testing.T) {
	slow := &mockSlowSource{"Slow", 200 * time.Millisecond}
	fast := &mockSource{"Fast", 10, 10, "Clear", false}
	sources := []WeatherSource{fast, slow}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	results := fetchWeatherConcurrently(ctx, "Test", sources)
	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}

	var found bool
	for _, r := range results {
		if r.Source == "Slow" {
			found = true
			if r.Error == nil {
				t.Fatalf("expected error from Slow, got nil")
			}
			if !errors.Is(r.Error, context.DeadlineExceeded) && !errors.Is(r.Error, context.Canceled) {
				t.Fatalf("expected context error, got %v", r.Error)
			}
		}
	}
	if !found {
		t.Fatalf("did not find Slow result")
	}
}
