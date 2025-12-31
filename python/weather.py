"""
Weather Data Aggregator - Core Module
Demonstrates Python's asyncio for concurrent API calls.
Comparable to Go's goroutines and channels implementation.

Semester Project: Parallel Programming - Go & Python
"""

import asyncio
import aiohttp
from dataclasses import dataclass
from typing import List, Dict, Optional, Tuple
from urllib.parse import quote

# =============================================================================
# Configuration
# =============================================================================

REQUEST_TIMEOUT = aiohttp.ClientTimeout(total=10)


# =============================================================================
# Data Structures
# =============================================================================

@dataclass
class WeatherData:
    """Weather information from a single source."""
    source: str
    temperature: float = 0.0
    humidity: Optional[float] = None
    condition: str = ""
    error: Optional[str] = None


# =============================================================================
# HTTP Helper Functions
# =============================================================================

async def http_get_json(url: str) -> Tuple[Optional[dict], Optional[str]]:
    """
    Perform HTTP GET and return JSON response.
    
    Returns:
        (data, None) on success, (None, error_message) on failure
    """
    try:
        async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:
            async with session.get(url) as resp:
                if resp.status != 200:
                    return None, f"status {resp.status}"
                return await resp.json(), None
    except asyncio.TimeoutError:
        return None, "timeout"
    except Exception as e:
        return None, str(e)


async def geocode_city(city: str) -> Tuple[Optional[Tuple[float, float]], Optional[str]]:
    """
    Resolve city name to (lat, lon) coordinates using Open-Meteo geocoding.
    
    Returns:
        ((lat, lon), None) on success, (None, error_message) on failure
    """
    url = f"https://geocoding-api.open-meteo.com/v1/search?name={quote(city)}&count=1"
    data, err = await http_get_json(url)
    
    if err:
        return None, f"geo: {err}"
    if not data or not data.get('results'):
        return None, "city not found"
    
    result = data['results'][0]
    return (result['latitude'], result['longitude']), None


# =============================================================================
# Weather Sources - Individual API Implementations
# =============================================================================

class OpenMeteoSource:
    """Open-Meteo API - Free, no API key required."""
    
    name = "Open-Meteo"
    
    async def fetch(self, city: str) -> WeatherData:
        result = WeatherData(source=self.name)
        
        # Geocoding
        coords, err = await geocode_city(city)
        if err:
            result.error = err
            return result
        
        lat, lon = coords
        url = (
            f"https://api.open-meteo.com/v1/forecast?"
            f"latitude={lat}&longitude={lon}&"
            f"current=temperature_2m,relative_humidity_2m,weather_code"
        )
        
        data, err = await http_get_json(url)
        if err:
            result.error = err
            return result
        
        current = data.get('current', {})
        result.temperature = current.get('temperature_2m', 0.0)
        result.humidity = current.get('relative_humidity_2m', 0.0)
        result.condition = _map_wmo_code(current.get('weather_code', 0))
        return result


class WttrinSource:
    """wttr.in API - Free, no API key required."""
    
    name = "wttr.in"
    
    async def fetch(self, city: str) -> WeatherData:
        result = WeatherData(source=self.name)
        
        data, err = await http_get_json(f"https://wttr.in/{quote(city)}?format=j1")
        if err:
            result.error = err
            return result
        
        conditions = data.get('current_condition', [])
        if not conditions:
            result.error = "no data"
            return result
        
        current = conditions[0]
        result.temperature = float(current.get('temp_C', 0))
        result.humidity = float(current.get('humidity', 0))
        
        desc = current.get('weatherDesc', [])
        if desc:
            result.condition = desc[0].get('value', '')
        
        return result


class WeatherAPISource:
    """WeatherAPI.com - Requires API key."""
    
    name = "WeatherAPI.com"
    
    def __init__(self, api_key: str):
        self._api_key = api_key
    
    async def fetch(self, city: str) -> WeatherData:
        result = WeatherData(source=self.name)
        
        if not self._api_key:
            result.error = "API key required"
            return result
        
        url = f"https://api.weatherapi.com/v1/current.json?key={self._api_key}&q={quote(city)}"
        data, err = await http_get_json(url)
        if err:
            result.error = err
            return result
        
        current = data.get('current', {})
        result.temperature = current.get('temp_c', 0.0)
        result.humidity = float(current.get('humidity', 0))
        result.condition = current.get('condition', {}).get('text', '')
        return result


class WeatherstackSource:
    """Weatherstack API - Requires API key. Free tier uses HTTP only."""
    
    name = "Weatherstack"
    
    def __init__(self, api_key: str):
        self._api_key = api_key
    
    async def fetch(self, city: str) -> WeatherData:
        result = WeatherData(source=self.name)
        
        if not self._api_key:
            result.error = "API key required"
            return result
        
        # Free tier requires HTTP, not HTTPS
        url = f"http://api.weatherstack.com/current?access_key={self._api_key}&query={quote(city)}"
        data, err = await http_get_json(url)
        if err:
            result.error = err
            return result
        
        current = data.get('current', {})
        result.temperature = float(current.get('temperature', 0))
        result.humidity = float(current.get('humidity', 0))
        
        descriptions = current.get('weather_descriptions', [])
        if descriptions:
            result.condition = descriptions[0]
        
        return result


class MeteosourceSource:
    """Meteosource API - Requires API key. Free tier may lack humidity."""
    
    name = "Meteosource"
    
    def __init__(self, api_key: str):
        self._api_key = api_key
    
    async def fetch(self, city: str) -> WeatherData:
        result = WeatherData(source=self.name)
        
        if not self._api_key:
            result.error = "API key required"
            return result
        
        url = (
            f"https://www.meteosource.com/api/v1/free/point?"
            f"place_id={quote(city)}&sections=current&"
            f"language=en&units=metric&key={self._api_key}"
        )
        data, err = await http_get_json(url)
        if err:
            result.error = err
            return result
        
        current = data.get('current', {})
        result.temperature = float(current.get('temperature', 0))
        result.condition = current.get('summary', '')
        
        # Humidity may be None in free tier
        if (humidity := current.get('humidity')) is not None:
            result.humidity = float(humidity)
        
        return result


class PirateWeatherSource:
    """Pirate Weather API - Dark Sky compatible, requires API key."""
    
    name = "Pirate Weather"
    
    def __init__(self, api_key: str):
        self._api_key = api_key
    
    async def fetch(self, city: str) -> WeatherData:
        result = WeatherData(source=self.name)
        
        if not self._api_key:
            result.error = "API key required"
            return result
        
        # Geocoding (reuses shared function)
        coords, err = await geocode_city(city)
        if err:
            result.error = err
            return result
        
        lat, lon = coords
        url = f"https://api.pirateweather.net/forecast/{self._api_key}/{lat},{lon}?units=si"
        data, err = await http_get_json(url)
        if err:
            result.error = err
            return result
        
        currently = data.get('currently', {})
        result.temperature = float(currently.get('temperature', 0))
        result.humidity = float(currently.get('humidity', 0)) * 100  # 0-1 → 0-100
        result.condition = currently.get('summary', '')
        return result


# =============================================================================
# Concurrent Fetching
# =============================================================================

async def fetch_weather_concurrently(city: str, sources: list) -> List[WeatherData]:
    """
    Fetch weather from all sources concurrently using asyncio.gather.
    Python's equivalent to Go's goroutines + channels pattern.
    """
    tasks = [source.fetch(city) for source in sources]
    results = await asyncio.gather(*tasks)
    return list(results)


async def fetch_weather_sequentially(city: str, sources: list) -> List[WeatherData]:
    """Fetch weather sequentially for performance comparison."""
    return [await source.fetch(city) for source in sources]


# =============================================================================
# Aggregation
# =============================================================================

def aggregate_weather(data: List[WeatherData]) -> Dict:
    """Calculate average temperature, humidity, and consensus condition."""
    if not data:
        return {'avg_temp': 0.0, 'avg_hum': 0.0, 'condition': 'No data', 'valid_count': 0}
    
    valid = [d for d in data if d.error is None]
    if not valid:
        return {'avg_temp': 0.0, 'avg_hum': 0.0, 'condition': 'No valid data', 'valid_count': 0}
    
    avg_temp = sum(d.temperature for d in valid) / len(valid)
    
    # Only average humidity from sources that provide it
    humidities = [d.humidity for d in valid if d.humidity is not None]
    avg_hum = sum(humidities) / len(humidities) if humidities else 0.0
    
    # Find most common normalized condition
    condition_counts: Dict[str, int] = {}
    for d in valid:
        normalized = normalize_condition(d.condition)
        condition_counts[normalized] = condition_counts.get(normalized, 0) + 1
    
    most_common = max(condition_counts, key=condition_counts.get)
    
    return {
        'avg_temp': avg_temp,
        'avg_hum': avg_hum,
        'condition': most_common,
        'valid_count': len(valid)
    }


# =============================================================================
# Utility Functions
# =============================================================================

def _map_wmo_code(code: int) -> str:
    """Map WMO weather codes to readable conditions."""
    if code == 0:
        return "Clear"
    if code <= 3:
        return "Partly Cloudy"
    if code <= 48:
        return "Foggy"
    if code <= 67:
        return "Rainy"
    if code <= 86:
        return "Snowy"
    return "Stormy"


# Condition normalization mapping
_CONDITION_MAP = [
    (['clear', 'sunny'], 'Clear'),
    (['partly'], 'Partly Cloudy'),
    (['cloud', 'overcast'], 'Cloudy'),
    (['rain', 'drizzle'], 'Rainy'),
    (['snow', 'sleet'], 'Snowy'),
    (['fog', 'mist'], 'Foggy'),
    (['storm', 'thunder'], 'Stormy'),
]


def normalize_condition(condition: str) -> str:
    """Normalize weather conditions to standard categories."""
    lower = condition.lower()
    for keywords, normalized in _CONDITION_MAP:
        if any(kw in lower for kw in keywords):
            return normalized
    return condition


# Emoji mapping
_EMOJI_MAP = {
    'clear': '☀️', 'sunny': '☀️',
    'partly': '⛅',
    'cloud': '☁️', 'overcast': '☁️',
    'rain': '🌧️', 'drizzle': '🌧️',
    'snow': '❄️', 'sleet': '❄️',
    'fog': '🌫️', 'mist': '🌫️',
    'storm': '⛈️', 'thunder': '⛈️',
}


def get_condition_emoji(condition: str) -> str:
    """Map weather condition to emoji."""
    lower = condition.lower()
    for key, emoji in _EMOJI_MAP.items():
        if key in lower:
            return emoji
    return '🌡️'
