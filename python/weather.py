"""Weather Data Aggregator - Async API fetching with Python asyncio."""

import asyncio
import aiohttp
import json
import os
import time
from dataclasses import dataclass
from pathlib import Path
from typing import List, Dict, Optional, Protocol
from urllib.parse import quote

# Configuration Constants
REQUEST_TIMEOUT = aiohttp.ClientTimeout(total=10)


# Custom Exceptions
class WeatherError(Exception):
    """Base exception for weather-related errors."""

    pass


class GeocodingError(WeatherError):
    """Raised when city geocoding fails."""

    pass


class APIError(WeatherError):
    """Raised when API request fails."""

    pass


# Global weather code mappings (loaded once at startup like Go)
_WEATHER_CODES_PATH = Path(__file__).parent.parent / "weather_codes.json"
weather_code_mapping: dict = {}
condition_mapping: dict = {}


def load_weather_codes() -> None:
    """Load weather code mappings from JSON."""
    global weather_code_mapping, condition_mapping
    with open(_WEATHER_CODES_PATH) as f:
        codes = json.load(f)
    weather_code_mapping = codes
    condition_mapping = codes.get("conditions", {})


@dataclass
class WeatherData:
    """Weather from a single source with temperature, humidity, condition."""

    source: str
    temperature: float = 0.0
    humidity: Optional[float] = None
    condition: str = ""
    error: Optional[str] = None
    duration_ms: Optional[float] = None  # Request duration in milliseconds


class WeatherSource(Protocol):
    """Weather source interface - implements fetch(city, session, coords_cache)."""

    name: str

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, tuple[float, float]]] = None,
    ) -> WeatherData: ...


def init_sources() -> list:
    """Initialize weather sources (free + API-key sources if configured)."""
    sources = [OpenMeteoSource()]

    # Add API-key sources if keys are available
    if key := os.getenv("TOMORROW_API_KEY"):
        sources.append(TomorrowIOSource(key))

    if key := os.getenv("WEATHER_API_COM_KEY"):
        sources.append(WeatherAPISource(key))

    if key := os.getenv("METEOSOURCE_API_KEY"):
        sources.append(MeteosourceSource(key))

    if key := os.getenv("PIRATE_WEATHER_API_KEY"):
        sources.append(PirateWeatherSource(key))

    return sources


def normalize_source(name: str) -> str:
    """Normalize source name: lowercase, remove spaces/dashes/dots."""
    return name.replace(" ", "").replace("-", "").replace(".", "").lower()


async def http_get_json(url: str, session: aiohttp.ClientSession) -> dict:
    """HTTP GET returning JSON data. Raises APIError on failure.

    Args:
        url: URL to fetch
        session: aiohttp client session

    Returns:
        Parsed JSON response as dictionary

    Raises:
        APIError: If request fails (timeout, network, HTTP error, invalid JSON)
    """
    try:
        async with session.get(url) as resp:
            if resp.status != 200:
                raise APIError(f"HTTP {resp.status}")
            return await resp.json()
    except asyncio.TimeoutError:
        raise APIError("timeout")
    except json.JSONDecodeError as e:
        raise APIError(f"invalid JSON: {str(e)}")
    except aiohttp.ClientError as e:
        raise APIError(f"request failed: {str(e)}")


async def geocode_city(
    city: str, session: aiohttp.ClientSession
) -> tuple[float, float]:
    """Resolve city name to (lat, lon) using Open-Meteo geocoding API.

    Args:
        city: City name to geocode
        session: aiohttp client session

    Returns:
        Tuple of (latitude, longitude)

    Raises:
        GeocodingError: If geocoding fails or city not found
    """
    url = f"https://geocoding-api.open-meteo.com/v1/search?name={quote(city)}&count=1"
    try:
        data = await http_get_json(url, session)
    except APIError as e:
        raise GeocodingError(f"geocoding request failed: {str(e)}") from e

    if not data or not data.get("results"):
        raise GeocodingError("city not found")

    result = data["results"][0]
    return (result["latitude"], result["longitude"])


async def get_coordinates(
    city: str,
    session: aiohttp.ClientSession,
    coords_cache: Optional[Dict[str, tuple[float, float]]] = None,
) -> tuple[float, float]:
    """Get coords from cache or geocode if not cached.

    Returns cached coordinates or performs geocoding.
    Raises GeocodingError if geocoding fails.
    """
    if coords_cache and city in coords_cache:
        return coords_cache[city]
    return await geocode_city(city, session)


class BaseAPISource:
    """Base class for API sources requiring authentication."""

    def __init__(self, api_key: str):
        self._api_key = api_key


class OpenMeteoSource:
    """Open-Meteo API - no key required."""

    name = "Open-Meteo"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)

        try:
            coords = await get_coordinates(city, session, coords_cache)
            lat, lon = coords

            url = (
                f"https://api.open-meteo.com/v1/forecast?"
                f"latitude={lat}&longitude={lon}&"
                f"current=temperature_2m,relative_humidity_2m,weather_code"
            )

            data = await http_get_json(url, session)
            current = data.get("current", {})

            result.temperature = float(current.get("temperature_2m", 0))
            result.humidity = (
                float(current.get("relative_humidity_2m"))
                if current.get("relative_humidity_2m") is not None
                else None
            )
            result.condition = _map_wmo_code(current.get("weather_code", 0))

        except (GeocodingError, APIError) as e:
            result.error = str(e)
        except (ValueError, TypeError, KeyError) as e:
            result.error = f"data parsing error: {str(e)}"

        return result


class TomorrowIOSource(BaseAPISource):
    """Tomorrow.io API - requires key, coordinate-based."""

    name = "Tomorrow.io"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)
        if not self._api_key:
            result.error = "API key required"
            return result

        try:
            coords = await get_coordinates(city, session, coords_cache)
            lat, lon = coords

            url = f"https://api.tomorrow.io/v4/weather/realtime?location={lat},{lon}&apikey={self._api_key}"
            data = await http_get_json(url, session)

            data_obj = data.get("data", {})
            values = data_obj.get("values", {})

            result.temperature = float(values.get("temperature", 0))
            result.humidity = (
                float(values.get("humidity"))
                if values.get("humidity") is not None
                else None
            )
            result.condition = values.get("weatherCode", "")
            if result.condition:
                result.condition = _map_tomorrow_code(result.condition)

        except (GeocodingError, APIError) as e:
            result.error = str(e)
        except (ValueError, TypeError, KeyError) as e:
            result.error = f"data parsing error: {str(e)}"

        return result


class WeatherAPISource(BaseAPISource):
    """WeatherAPI.com - requires key."""

    name = "WeatherAPI.com"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)
        if not self._api_key:
            result.error = "API key required"
            return result

        try:
            url = f"https://api.weatherapi.com/v1/current.json?key={self._api_key}&q={quote(city)}"
            data = await http_get_json(url, session)

            current = data.get("current", {})
            result.temperature = float(current.get("temp_c", 0))
            result.humidity = (
                float(current.get("humidity"))
                if current.get("humidity") is not None
                else None
            )
            result.condition = current.get("condition", {}).get("text", "")

        except APIError as e:
            result.error = str(e)
        except (ValueError, TypeError, KeyError) as e:
            result.error = f"data parsing error: {str(e)}"

        return result


class MeteosourceSource(BaseAPISource):
    """Meteosource API - requires key, no available humidity on free tier."""

    name = "Meteosource"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)
        if not self._api_key:
            result.error = "API key required"
            return result

        try:
            coords = await get_coordinates(city, session, coords_cache)
            lat, lon = coords

            url = (
                f"https://www.meteosource.com/api/v1/free/point?"
                f"lat={lat}&lon={lon}&sections=current&"
                f"language=en&units=metric&key={self._api_key}"
            )
            data = await http_get_json(url, session)

            current = data.get("current", {})
            result.temperature = float(current.get("temperature", 0))
            result.condition = current.get("summary", "")
            result.humidity = (
                float(current.get("humidity"))
                if current.get("humidity") is not None
                else None
            )

        except (GeocodingError, APIError) as e:
            result.error = str(e)
        except (ValueError, TypeError, KeyError) as e:
            result.error = f"data parsing error: {str(e)}"

        return result


class PirateWeatherSource(BaseAPISource):
    """Pirate Weather API - requires key."""

    name = "Pirate-Weather"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)
        if not self._api_key:
            result.error = "API key required"
            return result

        try:
            coords = await get_coordinates(city, session, coords_cache)
            lat, lon = coords

            url = f"https://api.pirateweather.net/forecast/{self._api_key}/{lat},{lon}?units=si"
            data = await http_get_json(url, session)

            currently = data.get("currently", {})
            result.temperature = float(currently.get("temperature", 0))

            humidity_raw = currently.get("humidity")
            result.humidity = (
                float(humidity_raw) * 100 if humidity_raw and humidity_raw > 0 else None
            )
            result.condition = currently.get("summary", "")

        except (GeocodingError, APIError) as e:
            result.error = str(e)
        except (ValueError, TypeError, KeyError) as e:
            result.error = f"data parsing error: {str(e)}"

        return result


async def _fetch_with_timing(
    source: WeatherSource,
    city: str,
    session: aiohttp.ClientSession,
    coords_cache: Dict[str, tuple[float, float]],
) -> WeatherData:
    """Fetch weather from a source and measure duration."""
    start = time.perf_counter()
    result = await source.fetch(city, session, coords_cache)
    result.duration_ms = (time.perf_counter() - start) * 1000
    return result


async def fetch_weather_concurrently(
    city: str, sources: List[WeatherSource]
) -> List[WeatherData]:
    """Fetch from all sources concurrently using asyncio.gather."""
    async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:
        coords_cache: Dict[str, tuple[float, float]] = {}

        try:
            coords = await geocode_city(city, session)
            coords_cache[city] = coords
        except GeocodingError:
            pass
        tasks = [
            _fetch_with_timing(source, city, session, coords_cache)
            for source in sources
        ]
        return await asyncio.gather(*tasks)


async def fetch_weather_sequentially(
    city: str, sources: List[WeatherSource]
) -> List[WeatherData]:
    """Fetch weather sequentially for performance comparison."""
    async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:
        coords_cache: Dict[str, tuple[float, float]] = {}

        try:
            coords = await geocode_city(city, session)
            coords_cache[city] = coords
        except GeocodingError:
            pass

        return [
            await _fetch_with_timing(source, city, session, coords_cache)
            for source in sources
        ]


def aggregate_weather(data: List[WeatherData]) -> Dict:
    """Calculate avg temp/humidity and consensus condition from valid data."""
    result = {
        "avg_temp": 0.0,
        "avg_hum": 0.0,
        "hum_count": 0,
        "condition": "No data",
        "valid_count": 0,
    }

    if not data:
        return result

    valid = [d for d in data if d.error is None]
    if not valid:
        result["condition"] = "No valid data"
        return result

    result["valid_count"] = len(valid)
    result["avg_temp"] = sum(d.temperature for d in valid) / len(valid)

    humidities = [d.humidity for d in valid if d.humidity is not None]
    if humidities:
        result["avg_hum"] = sum(humidities) / len(humidities)
        result["hum_count"] = len(humidities)

    condition_counts: Dict[str, int] = {}
    for d in valid:
        normalized = normalize_condition(d.condition)
        condition_counts[normalized] = condition_counts.get(normalized, 0) + 1

    if condition_counts:
        result["condition"] = max(condition_counts, key=condition_counts.get)
    else:
        result["condition"] = "Unknown"

    return result


def _map_wmo_code(code: int) -> str:
    """Map WMO weather codes to readable conditions."""
    for range_def in weather_code_mapping["wmo"]["ranges"]:
        if range_def["min"] <= code <= range_def["max"]:
            return range_def["condition"]
    return "Unknown"


def _map_tomorrow_code(code: str) -> str:
    """Map Tomorrow.io codes to readable conditions."""
    try:
        code_str = str(int(code))
    except (ValueError, TypeError):
        return "Unknown"
    return weather_code_mapping["tomorrow_io"].get(code_str, "Unknown")


def normalize_condition(condition: str) -> str:
    """Normalize conditions to standard categories via keyword matching.

    Checks more specific patterns first (e.g., 'Partly Cloudy' before 'Cloudy').
    """
    lower = condition.lower()

    # Check in priority order (most specific first)
    condition_order = [
        "Partly Cloudy",
        "Clear",
        "Cloudy",
        "Rainy",
        "Snowy",
        "Foggy",
        "Stormy",
    ]

    for normalized in condition_order:
        if normalized in condition_mapping:
            if any(kw in lower for kw in condition_mapping[normalized]["keywords"]):
                return normalized

    return condition


def get_condition_emoji(condition: str) -> str:
    """Map condition to emoji. Returns thermometer if no match."""
    lower = condition.lower()
    for normalized, info in condition_mapping.items():
        if normalized.lower() in lower:
            return info["emoji"]
        if any(kw in lower for kw in info["keywords"]):
            return info["emoji"]
    return "🌡️"
