"""Weather Aggregator Tests - Core Functionality"""

import pytest
from weather import WeatherData, aggregate_weather, normalize_condition
from unittest.mock import MagicMock, AsyncMock


class TestHttpGetJson:
    """Tests for http_get_json error handling."""

    @pytest.mark.asyncio
    async def test_http_error(self):
        """Test non-200 status code."""
        from weather import http_get_json

        mock_resp = MagicMock()
        mock_resp.status = 404
        mock_resp.__aenter__ = AsyncMock(return_value=mock_resp)
        mock_resp.__aexit__ = AsyncMock()

        session = MagicMock()
        session.get.return_value = mock_resp

        data, err = await http_get_json("http://test.com", session)

        assert data is None
        assert err == "HTTP 404"


class TestValidateCityName:
    """Tests for city name validation."""

    @pytest.mark.parametrize(
        "city,expected",
        [
            ("Munich", "Munich"),
            ("  Berlin  ", "Berlin"),
            ("", None),
            ("123", None),
            ("A" * 101, None),
        ],
    )
    def test_validation(self, city, expected):
        from main import validate_city_name

        assert validate_city_name(city) == expected


class TestAggregateWeather:
    """Tests for weather data aggregation."""

    def test_all_valid(self):
        """Average calculation with all valid sources."""
        data = [
            WeatherData(
                source="A", temperature=15.0, humidity=60.0, condition="Cloudy"
            ),
            WeatherData(
                source="B", temperature=17.0, humidity=70.0, condition="Cloudy"
            ),
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
        assert result["condition"] == "No valid data"

    def test_condition_consensus(self):
        """Most common normalized condition wins."""
        data = [
            WeatherData(source="A", temperature=10.0, humidity=50.0, condition="Clear"),
            WeatherData(source="B", temperature=11.0, humidity=55.0, condition="Sunny"),
            WeatherData(
                source="C", temperature=12.0, humidity=60.0, condition="Cloudy"
            ),
        ]
        result = aggregate_weather(data)

        assert result["condition"] == "Clear"


class TestNormalizeCondition:
    """Tests for condition normalization."""

    @pytest.mark.parametrize(
        "raw,expected",
        [
            ("Clear sky", "Clear"),
            ("Sunny", "Clear"),
            ("Partly cloudy", "Partly Cloudy"),
            ("Light rain", "Rainy"),
            ("Snow showers", "Snowy"),
            ("Thunderstorm", "Stormy"),
            ("Unknown xyz", "Unknown xyz"),
        ],
    )
    def test_normalization(self, raw: str, expected: str):
        assert normalize_condition(raw) == expected


class MockSource:
    """Mock weather source for testing."""

    def __init__(
        self,
        name: str,
        temp: float,
        hum: float | None,
        cond: str,
        error: str | None = None,
    ):
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
async def test_fetch_concurrent():
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
    assert all(r.duration_ms is not None for r in results)
