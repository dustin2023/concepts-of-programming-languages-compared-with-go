"""
Weather Aggregator Tests

Run with: pytest test_weather.py -v
"""

import pytest
from weather import (
    WeatherData,
    aggregate_weather,
    normalize_condition,
    get_condition_emoji,
)


# =============================================================================
# Test aggregate_weather()
# =============================================================================


class TestAggregateWeather:
    """Tests for weather data aggregation."""

    def test_all_valid(self):
        """Average calculation with all valid sources."""
        data = [
            WeatherData(source="A", temperature=15.0, humidity=60.0, condition="Cloudy"),
            WeatherData(source="B", temperature=17.0, humidity=70.0, condition="Cloudy"),
        ]
        result = aggregate_weather(data)

        assert result["valid_count"] == 2
        assert result["avg_temp"] == 16.0
        assert result["avg_hum"] == 65.0
        assert result["condition"] == "Cloudy"

    def test_mixed_errors(self):
        """Aggregation excludes sources with errors."""
        data = [
            WeatherData(source="A", temperature=20.0, humidity=50.0, condition="Clear"),
            WeatherData(source="B", error="timeout"),
        ]
        result = aggregate_weather(data)

        assert result["valid_count"] == 1
        assert result["avg_temp"] == 20.0

    def test_all_errors(self):
        """All sources failed."""
        data = [
            WeatherData(source="A", error="timeout"),
            WeatherData(source="B", error="HTTP 500"),
        ]
        result = aggregate_weather(data)

        assert result["valid_count"] == 0
        assert result["hum_count"] == 0
        assert result["condition"] == "No valid data"

    def test_empty_input(self):
        """Empty input list."""
        result = aggregate_weather([])

        assert result["valid_count"] == 0
        assert result["hum_count"] == 0
        assert result["condition"] == "No data"

    def test_condition_consensus(self):
        """Most common normalized condition wins."""
        data = [
            WeatherData(source="A", temperature=10.0, humidity=50.0, condition="Clear"),
            WeatherData(source="B", temperature=11.0, humidity=55.0, condition="Sunny"),  # → Clear
            WeatherData(source="C", temperature=12.0, humidity=60.0, condition="Cloudy"),
        ]
        result = aggregate_weather(data)

        assert result["condition"] == "Clear"  # Clear + Sunny (normalized) = 2


# =============================================================================
# Test normalize_condition()
# =============================================================================


class TestNormalizeCondition:
    """Tests for condition normalization."""

    @pytest.mark.parametrize(
        "raw,expected",
        [
            ("Clear sky", "Clear"),
            ("Sunny", "Clear"),
            ("Partly cloudy", "Partly Cloudy"),
            ("Overcast", "Cloudy"),
            ("Light rain", "Rainy"),
            ("Heavy drizzle", "Rainy"),
            ("Snow showers", "Snowy"),
            ("Fog", "Foggy"),
            ("Mist", "Foggy"),
            ("Thunderstorm", "Stormy"),
            ("Unknown xyz", "Unknown xyz"),  # Passthrough
            ("", ""),  # Empty → Empty (not Unknown)
        ],
    )
    def test_normalization(self, raw: str, expected: str):
        assert normalize_condition(raw) == expected


# =============================================================================
# Test get_emoji()
# =============================================================================


class TestGetConditionEmoji:
    """Tests for emoji mapping."""

    @pytest.mark.parametrize(
        "condition,expected",
        [
            ("Clear", "☀️"),
            ("Sunny", "☀️"),
            ("Partly Cloudy", "⛅"),
            ("Cloudy", "☁️"),
            ("Rainy", "🌧️"),
            ("Snowy", "❄️"),
            ("Foggy", "🌫️"),
            ("Stormy", "⛈️"),
            ("Unknown", "🌡️"),
        ],
    )
    def test_emoji(self, condition: str, expected: str):
        assert get_condition_emoji(condition) == expected


# =============================================================================
# Test WeatherData
# =============================================================================


class TestWeatherData:
    """Tests for WeatherData dataclass."""

    def test_defaults(self):
        """Default values."""
        data = WeatherData(source="Test")

        assert data.source == "Test"
        assert data.temperature == 0.0
        assert data.humidity is None
        assert data.condition == ""
        assert data.error is None
        assert data.duration_ms is None

    def test_with_error(self):
        """Error state."""
        data = WeatherData(source="API", error="connection failed")

        assert data.error == "connection failed"


# =============================================================================
# Integration: Mock Sources
# =============================================================================


class MockSource:
    """Mock weather source for testing fetch functions."""

    def __init__(self, name: str, temp: float, hum: float | None, cond: str, error: str | None = None):
        self.name = name
        self._temp = temp
        self._hum = hum
        self._cond = cond
        self._error = error

    async def fetch(self, city: str, session, coords_cache=None):
        if self._error:
            return WeatherData(source=self.name, error=self._error)
        return WeatherData(
            source=self.name,
            temperature=self._temp,
            humidity=self._hum,
            condition=self._cond,
        )


@pytest.mark.asyncio
async def test_fetch_all_concurrent():
    """Concurrent fetching with mock sources."""
    from weather import fetch_weather_concurrently

    sources = [
        MockSource("A", 15.0, 60.0, "Clear"),
        MockSource("B", 16.0, 65.0, "Cloudy"),
        MockSource("C", 0.0, None, "", error="timeout"),
    ]
    results = await fetch_weather_concurrently("Test", sources)

    assert len(results) == 3
    assert sum(1 for r in results if r.error is None) == 2


@pytest.mark.asyncio
async def test_fetch_all_sequential():
    """Sequential fetching with mock sources."""
    from weather import fetch_weather_sequentially

    sources = [
        MockSource("A", 20.0, 50.0, "Sunny"),
        MockSource("B", 21.0, 55.0, "Clear"),
    ]
    results = await fetch_weather_sequentially("Test", sources)

    assert len(results) == 2
    assert results[0].temperature == 20.0
    assert results[1].temperature == 21.0
