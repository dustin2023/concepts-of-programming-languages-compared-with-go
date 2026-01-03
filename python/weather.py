"""Weather Data Aggregator - Core Module

Demonstrates Python's asyncio for concurrent API calls.
Comparable to Go's goroutines and channels implementation.
"""

import asyncio
import aiohttp
import json
import time
from dataclasses import dataclass
from pathlib import Path
from typing import List, Dict, Optional, Tuple, Protocol
from urllib.parse import quote

# Configuration Constants
MAX_CITY_NAME_LENGTH = 100
REQUEST_TIMEOUT = aiohttp.ClientTimeout(total=10)

# Load weather code mappings from shared JSON file
_WEATHER_CODES_PATH = Path(__file__).parent.parent / "weather_codes.json"
with open(_WEATHER_CODES_PATH) as f:
    _WEATHER_CODES = json.load(f)


@dataclass
class WeatherData:
    """Weather information from a single source."""

    source: str
    temperature: float = 0.0
    humidity: Optional[float] = None
    condition: str = ""
    error: Optional[str] = None
    duration_ms: Optional[float] = None  # Request duration in milliseconds


class WeatherSource(Protocol):
    """Protocol for weather source implementations."""

    name: str

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
    ) -> WeatherData: ...


def safe_float(value, default: float = 0.0) -> float:
    """Safely convert a value to float, returning a default on any error.

    Handles None, non-numeric strings, and type mismatches gracefully.
    """
    if value is None:
        return default
    try:
        return float(value)
    except (ValueError, TypeError):
        return default


async def http_get_json(
    url: str, session: aiohttp.ClientSession
) -> Tuple[Optional[dict], Optional[str]]:
    """Perform HTTP GET request and return parsed JSON response.

    Args:
        url: The URL to fetch
        session: aiohttp ClientSession with timeout configured

    Returns:
        Tuple of (parsed_json, error_string). If successful, error is None.
        Returns (None, error_string) on any failure.
    """
    try:
        async with session.get(url) as resp:
            if resp.status != 200:
                return None, f"HTTP {resp.status}"
            return await resp.json(), None
    except asyncio.TimeoutError:
        return None, "timeout"
    except aiohttp.ClientConnectorError as e:
        return None, f"connection failed: {str(e)}"
    except aiohttp.ServerTimeoutError:
        return None, "server timeout"
    except aiohttp.ClientError as e:
        return None, f"network error: {type(e).__name__}"
    except ValueError:
        return None, "invalid JSON"
    except Exception as e:
        return None, f"error: {type(e).__name__}"


async def geocode_city(
    city: str, session: aiohttp.ClientSession
) -> Tuple[Optional[Tuple[float, float]], Optional[str]]:
    """Resolve city name to (lat, lon) coordinates."""
    url = f"https://geocoding-api.open-meteo.com/v1/search?name={quote(city)}&count=1"
    data, err = await http_get_json(url, session)

    if err:
        return None, err
    if not data or not data.get("results"):
        return None, "city not found"

    result = data["results"][0]
    return (result["latitude"], result["longitude"]), None


async def get_coordinates(
    city: str,
    session: aiohttp.ClientSession,
    coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
) -> Tuple[Optional[Tuple[float, float]], Optional[str]]:
    """Get coordinates from cache or geocode."""
    if coords_cache and city in coords_cache:
        return coords_cache[city], None
    return await geocode_city(city, session)


class BaseAPISource:
    """Base class for API sources requiring authentication."""

    def __init__(self, api_key: str):
        self._api_key = api_key

    def _validate_api_key(self, result: WeatherData) -> bool:
        if not self._api_key:
            result.error = "API key required"
            return False
        return True


class OpenMeteoSource:
    """Open-Meteo API - Free, no API key required."""

    name = "Open-Meteo"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)

        coords, err = await get_coordinates(city, session, coords_cache)
        if err:
            result.error = err
            return result

        lat, lon = coords
        url = (
            f"https://api.open-meteo.com/v1/forecast?"
            f"latitude={lat}&longitude={lon}&"
            f"current=temperature_2m,relative_humidity_2m,weather_code"
        )

        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result

        current = data.get("current", {})
        result.temperature = safe_float(current.get("temperature_2m"))
        result.humidity = safe_float(current.get("relative_humidity_2m"))
        result.condition = _map_wmo_code(current.get("weather_code", 0))
        return result


class TomorrowIOSource(BaseAPISource):
    """Tomorrow.io API - Requires API key. Global weather data with coordinates."""

    name = "Tomorrow.io"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)

        if not self._validate_api_key(result):
            return result

        coords, err = await get_coordinates(city, session, coords_cache)
        if err:
            result.error = err
            return result

        lat, lon = coords
        url = f"https://api.tomorrow.io/v4/weather/realtime?location={lat},{lon}&apikey={self._api_key}"
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result

        # Extract nested data safely - .get() handles missing keys gracefully
        data_obj = data.get("data", {})
        values = data_obj.get("values", {})

        result.temperature = safe_float(values.get("temperature"))
        result.humidity = safe_float(values.get("humidity"))
        result.condition = values.get("weatherCode", "")
        if result.condition:
            result.condition = _map_tomorrow_code(result.condition)

        return result


class WeatherAPISource(BaseAPISource):
    """WeatherAPI.com - Requires API key."""

    name = "WeatherAPI.com"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)

        if not self._validate_api_key(result):
            return result

        url = f"https://api.weatherapi.com/v1/current.json?key={self._api_key}&q={quote(city)}"
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result

        current = data.get("current", {})
        result.temperature = safe_float(current.get("temp_c"))
        result.humidity = safe_float(current.get("humidity"))
        result.condition = current.get("condition", {}).get("text", "")
        return result


class WeatherstackSource(BaseAPISource):
    """Weatherstack API - Requires API key. Free tier uses HTTP only."""

    name = "Weatherstack"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)

        if not self._validate_api_key(result):
            return result

        url = f"http://api.weatherstack.com/current?access_key={self._api_key}&query={quote(city)}"
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result

        current = data.get("current", {})
        result.temperature = safe_float(current.get("temperature"))
        result.humidity = safe_float(current.get("humidity"))

        descriptions = current.get("weather_descriptions", [])
        if descriptions:
            result.condition = descriptions[0]

        return result


class MeteosourceSource(BaseAPISource):
    """Meteosource API - Requires API key. Free tier may lack humidity."""

    name = "Meteosource"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)

        if not self._validate_api_key(result):
            return result

        coords, err = await get_coordinates(city, session, coords_cache)
        if err:
            result.error = err
            return result

        lat, lon = coords
        url = (
            f"https://www.meteosource.com/api/v1/free/point?"
            f"lat={lat}&lon={lon}&sections=current&"
            f"language=en&units=metric&key={self._api_key}"
        )
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result

        current = data.get("current", {})
        result.temperature = safe_float(current.get("temperature"))
        result.condition = current.get("summary", "")

        # Humidity may be None in free tier
        humidity = current.get("humidity")
        if humidity is not None:
            result.humidity = safe_float(humidity)

        return result


class PirateWeatherSource(BaseAPISource):
    """Pirate Weather API - Dark Sky compatible, requires API key."""

    name = "Pirate Weather"

    async def fetch(
        self,
        city: str,
        session: aiohttp.ClientSession,
        coords_cache: Optional[Dict[str, Tuple[float, float]]] = None,
    ) -> WeatherData:
        result = WeatherData(source=self.name)

        if not self._validate_api_key(result):
            return result

        coords, err = await get_coordinates(city, session, coords_cache)
        if err:
            result.error = err
            return result

        lat, lon = coords
        url = f"https://api.pirateweather.net/forecast/{self._api_key}/{lat},{lon}?units=si"
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result

        currently = data.get("currently", {})
        result.temperature = safe_float(currently.get("temperature"))
        humidity_raw = safe_float(currently.get("humidity"))
        result.humidity = humidity_raw * 100 if humidity_raw > 0 else 0.0
        result.condition = currently.get("summary", "")
        return result


async def _fetch_with_timing(
    source: WeatherSource,
    city: str,
    session: aiohttp.ClientSession,
    coords_cache: Dict[str, Tuple[float, float]],
) -> WeatherData:
    """Fetch weather from a source and measure duration.
    
    Helper function used by both concurrent and sequential fetch functions.
    """
    start = time.perf_counter()
    result = await source.fetch(city, session, coords_cache)
    result.duration_ms = (time.perf_counter() - start) * 1000
    return result


async def fetch_weather_concurrently(
    city: str, sources: List[WeatherSource]
) -> List[WeatherData]:
    """Fetch weather from all sources concurrently using asyncio.gather."""
    async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:       # TODO maybe create function for session creation
        coords_cache: Dict[str, Tuple[float, float]] = {}
        coords, err = await geocode_city(city, session)
        if not err:
            coords_cache[city] = coords

        tasks = [_fetch_with_timing(source, city, session, coords_cache) for source in sources]
        return await asyncio.gather(*tasks)


async def fetch_weather_sequentially(
    city: str, sources: List[WeatherSource]
) -> List[WeatherData]:
    """Fetch weather sequentially for performance comparison."""
    async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:   # TODO maybe create function for session creation
        coords_cache: Dict[str, Tuple[float, float]] = {}
        coords, err = await geocode_city(city, session)
        if not err:
            coords_cache[city] = coords

        return [await _fetch_with_timing(source, city, session, coords_cache) for source in sources]


def aggregate_weather(data: List[WeatherData]) -> Dict:
    """Calculate average temperature, humidity, and consensus condition."""
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
        if normalized:
            condition_counts[normalized] = condition_counts.get(normalized, 0) + 1

    if condition_counts:
        result["condition"] = max(condition_counts, key=condition_counts.get)
    else:
        result["condition"] = "Unknown"

    return result


def _map_wmo_code(code: int) -> str:
    """Map WMO weather codes to readable conditions using range-based mapping."""
    for range_def in _WEATHER_CODES["wmo"]["ranges"]:
        if range_def["min"] <= code <= range_def["max"]:
            return range_def["condition"]
    return "Unknown"


def _map_tomorrow_code(code: str) -> str:
    """Map Tomorrow.io weather codes to readable conditions using shared mapping."""
    try:
        code_str = str(int(code))
    except (ValueError, TypeError):
        return "Unknown"
    return _WEATHER_CODES["tomorrow_io"].get(code_str, "Unknown")


# Unified condition mapping: Normalized name -> (keywords, emoji)
CONDITION_INFO = {
    "Clear": (["clear", "sunny"], "☀️"),
    "Partly Cloudy": (["partly"], "⛅"),
    "Cloudy": (["cloud", "overcast"], "☁️"),
    "Rainy": (["rain", "drizzle"], "🌧️"),
    "Snowy": (["snow", "sleet"], "❄️"),
    "Foggy": (["fog", "mist"], "🌫️"),
    "Stormy": (["storm", "thunder"], "⛈️"),
}


def normalize_condition(condition: str) -> str:
    """Normalize weather conditions to standard categories.

    Maps various weather descriptions to standard conditions using keyword matching.
    Uses shared condition mappings loaded from weather_codes.json.
    """
    lower = condition.lower()
    for normalized, (keywords, _) in CONDITION_INFO.items():
        if any(kw in lower for kw in keywords):
            return normalized
    return condition


def get_condition_emoji(condition: str) -> str:
    """Map weather condition to emoji for better visualization.

    Uses the shared condition mappings from weather_codes.json to provide
    consistent emoji representation across languages and APIs.
    Returns a default thermometer emoji if no match is found.
    """
    lower = condition.lower()

    for normalized, (keywords, emoji) in CONDITION_INFO.items():
        if normalized.lower() in lower:
            return emoji
        if any(kw in lower for kw in keywords):
            return emoji

    return "🌡️"
