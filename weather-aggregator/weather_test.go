package main

import "testing"

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

func TestFetchWeatherConcurrently(t *testing.T) {
	sources := []WeatherSource{
		&mockSource{"Mock1", 15, 60, "Clear", false},
		&mockSource{"Mock2", 16, 65, "Cloudy", false},
		&mockSource{"Mock3", 0, 0, "", true},
	}
	
	results := fetchWeatherConcurrently("Test", sources)
	
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

func BenchmarkFetchWeatherConcurrently(b *testing.B) {
	sources := []WeatherSource{
		&mockSource{"M1", 15, 60, "Clear", false},
		&mockSource{"M2", 16, 65, "Cloudy", false},
	}
	
	for i := 0; i < b.N; i++ {
		fetchWeatherConcurrently("Test", sources)
	}
}

func BenchmarkAggregateWeather(b *testing.B) {
	data := []WeatherData{
		{Source: "A", Temperature: 15, Humidity: 60, Condition: "Cloudy"},
		{Source: "B", Temperature: 16, Humidity: 65, Condition: "Cloudy"},
	}
	
	for i := 0; i < b.N; i++ {
		AggregateWeather(data)
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

func (m *mockSource) Fetch(city string) WeatherData {
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

type testError struct{}

func (e *testError) Error() string { return "test error" }
