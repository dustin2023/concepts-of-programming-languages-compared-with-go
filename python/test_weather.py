"""Weather Aggregator Tests"""

import pytest
from weather import WeatherData, aggregate_weather, normalize_condition


class TestAggregateWeather:
    def test_all_valid(self):
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
        assert result["condition"] == "Cloudy"

    def test_mixed_errors(self):
        data = [
            WeatherData(source="A", temperature=20.0, humidity=50.0, condition="Clear"),
            WeatherData(source="B", error="timeout"),
        ]
        result = aggregate_weather(data)
        assert result["valid_count"] == 1

    def test_all_errors(self):
        data = [
            WeatherData(source="A", error="timeout"),
            WeatherData(source="B", error="HTTP 500"),
        ]
        result = aggregate_weather(data)
        assert result["valid_count"] == 0


@pytest.mark.parametrize(
    "raw,expected",
    [
        ("Clear sky", "Clear"),
        ("Partly cloudy", "Partly Cloudy"),
        ("Light rain", "Rainy"),
        ("Snow", "Snowy"),
    ],
)
def test_normalize_condition(raw: str, expected: str):
    assert normalize_condition(raw) == expected


@pytest.mark.parametrize(
    "city,expected",
    [("Munich", "Munich"), ("  Berlin  ", "Berlin"), ("", None), ("A" * 101, None)],
)
def test_validate_city_name(city, expected):
    from main import validate_city_name

    assert validate_city_name(city) == expected


class MockSource:
    def __init__(
        self,
        name: str,
        temp: float,
        hum: float | None,
        cond: str,
        error: str | None = None,
    ):
        self.name = name
        self._temp, self._hum, self._cond, self._error = temp, hum, cond, error

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
    from weather import fetch_weather_concurrently

    sources = [
        MockSource("A", 15.0, 60.0, "Clear"),
        MockSource("B", 16.0, 65.0, "Cloudy"),
        MockSource("C", 0.0, None, "", error="timeout"),
    ]
    results = await fetch_weather_concurrently("Test", sources)
    assert len(results) == 3
    assert sum(1 for r in results if r.error is None) == 2
