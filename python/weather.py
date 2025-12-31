"""
Weather Data Aggregator - Core Module
Demonstrates Python's asyncio for concurrent API calls.
Comparable to Go's goroutines and channels implementation.

Semester Project: Parallel Programming - Go & Python
"""

import asyncio
import aiohttp
from dataclasses import dataclass
from typing import List, Dict, Optional, Tuple, Protocol
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
    duration_ms: Optional[float] = None  # Request duration in milliseconds


class WeatherSource(Protocol):
    """Protocol for weather source implementations."""
    name: str
    
    async def fetch(self, city: str, session: aiohttp.ClientSession, 
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
        ...


class TimedWeatherSource:
    """Wrapper to add timing to weather source fetches."""
    
    def __init__(self, source: WeatherSource):
        self.source = source
        self.name = source.name
    
    async def fetch(self, city: str, session: aiohttp.ClientSession,
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
        """Fetch weather data and track duration."""
        import time
        start = time.perf_counter()
        result = await self.source.fetch(city, session, coords_cache)
        duration = (time.perf_counter() - start) * 1000  # Convert to ms
        result.duration_ms = duration
        return result


# =============================================================================
# HTTP Helper Functions
# =============================================================================

def safe_float(value, default: float = 0.0) -> float:
    """Safely convert value to float, return default if invalid."""
    if value is None:
        return default
    try:
        return float(value)
    except (ValueError, TypeError):
        return default


async def http_get_json(url: str, session: aiohttp.ClientSession) -> Tuple[Optional[dict], Optional[str]]:
    """
    Perform HTTP GET and return JSON response.
    
    Returns:
        (data, None) on success, (None, error_message) on failure
    """
    try:
        async with session.get(url) as resp:
            if resp.status != 200:
                return None, f"HTTP {resp.status}"
            return await resp.json(), None
    except asyncio.TimeoutError:
        return None, "timeout"
    except aiohttp.ClientError as e:
        return None, f"network error: {e.__class__.__name__}"
    except ValueError:
        return None, "invalid JSON"
    except Exception as e:
        return None, f"unexpected: {e.__class__.__name__}"


async def geocode_city(city: str, session: aiohttp.ClientSession) -> Tuple[Optional[Tuple[float, float]], Optional[str]]:
    """
    Resolve city name to (lat, lon) coordinates using Open-Meteo geocoding.
    
    Returns:
        ((lat, lon), None) on success, (None, error_message) on failure
    """
    url = f"https://geocoding-api.open-meteo.com/v1/search?name={quote(city)}&count=1"
    data, err = await http_get_json(url, session)
    
    if err:
        return None, err
    if not data or not data.get('results'):
        return None, "city not found"
    
    result = data['results'][0]
    return (result['latitude'], result['longitude']), None


async def get_coordinates(city: str, session: aiohttp.ClientSession,
                         coords_cache: Optional[Dict[str, Tuple[float, float]]] = None
                         ) -> Tuple[Optional[Tuple[float, float]], Optional[str]]:
    """Get coordinates from cache or geocode."""
    if coords_cache and city in coords_cache:
        return coords_cache[city], None
    return await geocode_city(city, session)


# =============================================================================
# Weather Sources - Individual API Implementations
# =============================================================================

class BaseAPISource:
    """Base class for API sources requiring authentication."""
    
    def __init__(self, api_key: str):
        self._api_key = api_key
    
    def _validate_api_key(self, result: WeatherData) -> bool:
        """Return False and set error if API key is missing."""
        if not self._api_key:
            result.error = "API key required"
            return False
        return True

class OpenMeteoSource:
    """Open-Meteo API - Free, no API key required."""
    
    name = "Open-Meteo"
    
    async def fetch(self, city: str, session: aiohttp.ClientSession,
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
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
        
        current = data.get('current', {})
        result.temperature = safe_float(current.get('temperature_2m'))
        result.humidity = safe_float(current.get('relative_humidity_2m'))
        result.condition = _map_wmo_code(current.get('weather_code', 0))
        return result


class WttrinSource:
    """wttr.in API - Free, no API key required."""
    
    name = "wttr.in"
    
    async def fetch(self, city: str, session: aiohttp.ClientSession,
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
        result = WeatherData(source=self.name)
        
        data, err = await http_get_json(f"https://wttr.in/{quote(city)}?format=j1", session)
        if err:
            result.error = err
            return result
        
        try:
            conditions = data.get('current_condition', [])
            if not conditions or not isinstance(conditions, list):
                result.error = "invalid response format"
                return result
            
            current = conditions[0]
            if not current or not isinstance(current, dict):
                result.error = "invalid response format"
                return result
            
            result.temperature = safe_float(current.get('temp_C'))
            result.humidity = safe_float(current.get('humidity'))
            
            desc = current.get('weatherDesc', [])
            if desc and isinstance(desc, list) and desc[0]:
                result.condition = desc[0].get('value', '')
        except (KeyError, IndexError, TypeError) as e:
            result.error = f"parse error: {e.__class__.__name__}"
        
        return result


class WeatherAPISource(BaseAPISource):
    """WeatherAPI.com - Requires API key."""
    
    name = "WeatherAPI.com"
    
    async def fetch(self, city: str, session: aiohttp.ClientSession,
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
        result = WeatherData(source=self.name)
        
        if not self._validate_api_key(result):
            return result
        
        url = f"https://api.weatherapi.com/v1/current.json?key={self._api_key}&q={quote(city)}"
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result
        
        current = data.get('current', {})
        result.temperature = safe_float(current.get('temp_c'))
        result.humidity = safe_float(current.get('humidity'))
        result.condition = current.get('condition', {}).get('text', '')
        return result


class WeatherstackSource(BaseAPISource):
    """Weatherstack API - Requires API key. Free tier uses HTTP only."""
    
    name = "Weatherstack"
    
    async def fetch(self, city: str, session: aiohttp.ClientSession,
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
        result = WeatherData(source=self.name)
        
        if not self._validate_api_key(result):
            return result
        
        # Free tier requires HTTP, not HTTPS
        url = f"http://api.weatherstack.com/current?access_key={self._api_key}&query={quote(city)}"
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result
        
        current = data.get('current', {})
        result.temperature = safe_float(current.get('temperature'))
        result.humidity = safe_float(current.get('humidity'))
        
        descriptions = current.get('weather_descriptions', [])
        if descriptions:
            result.condition = descriptions[0]
        
        return result


class MeteosourceSource(BaseAPISource):
    """Meteosource API - Requires API key. Free tier may lack humidity."""
    
    name = "Meteosource"
    
    async def fetch(self, city: str, session: aiohttp.ClientSession,
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
        result = WeatherData(source=self.name)
        
        if not self._validate_api_key(result):
            return result
        
        url = (
            f"https://www.meteosource.com/api/v1/free/point?"
            f"place_id={quote(city)}&sections=current&"
            f"language=en&units=metric&key={self._api_key}"
        )
        data, err = await http_get_json(url, session)
        if err:
            result.error = err
            return result
        
        current = data.get('current', {})
        result.temperature = safe_float(current.get('temperature'))
        result.condition = current.get('summary', '')
        
        # Humidity may be None in free tier
        humidity = current.get('humidity')
        if humidity is not None:
            result.humidity = safe_float(humidity)
        
        return result


class PirateWeatherSource(BaseAPISource):
    """Pirate Weather API - Dark Sky compatible, requires API key."""
    
    name = "Pirate Weather"
    
    async def fetch(self, city: str, session: aiohttp.ClientSession,
                   coords_cache: Optional[Dict[str, Tuple[float, float]]] = None) -> WeatherData:
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
        
        currently = data.get('currently', {})
        result.temperature = safe_float(currently.get('temperature'))
        humidity_raw = safe_float(currently.get('humidity'))
        result.humidity = humidity_raw * 100 if humidity_raw > 0 else 0.0  # 0-1 → 0-100
        result.condition = currently.get('summary', '')
        return result


# =============================================================================
# Concurrent Fetching
# =============================================================================

async def fetch_weather_concurrently(city: str, sources: List[WeatherSource]) -> List[WeatherData]:
    """
    Fetch weather from all sources concurrently using asyncio.gather.
    Python's equivalent to Go's goroutines + channels pattern.
    Uses shared session and geocoding cache for efficiency.
    """
    async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:
        # Pre-geocode city if any source might need it
        coords_cache: Dict[str, Tuple[float, float]] = {}
        coords, err = await geocode_city(city, session)
        if not err:
            coords_cache[city] = coords
        
        # Wrap sources with timing
        timed_sources = [TimedWeatherSource(source) for source in sources]
        tasks = [source.fetch(city, session, coords_cache) for source in timed_sources]
        results = await asyncio.gather(*tasks)
        return results


async def fetch_weather_sequentially(city: str, sources: List[WeatherSource]) -> List[WeatherData]:
    """Fetch weather sequentially for performance comparison."""
    async with aiohttp.ClientSession(timeout=REQUEST_TIMEOUT) as session:
        # Pre-geocode city if any source might need it
        coords_cache: Dict[str, Tuple[float, float]] = {}
        coords, err = await geocode_city(city, session)
        if not err:
            coords_cache[city] = coords
        
        # Wrap sources with timing
        timed_sources = [TimedWeatherSource(source) for source in sources]
        return [await source.fetch(city, session, coords_cache) for source in timed_sources]


# =============================================================================
# Aggregation
# =============================================================================

def aggregate_weather(data: List[WeatherData]) -> Dict:
    """Calculate average temperature, humidity, and consensus condition."""
    # Default result
    result = {
        'avg_temp': 0.0,
        'avg_hum': 0.0,
        'hum_count': 0,
        'condition': 'No data',
        'valid_count': 0
    }
    
    if not data:
        return result
    
    valid = [d for d in data if d.error is None]
    if not valid:
        result['condition'] = 'No valid data'
        return result
    
    # Calculate aggregates
    result['valid_count'] = len(valid)
    result['avg_temp'] = sum(d.temperature for d in valid) / len(valid)
    
    # Only average humidity from sources that provide it
    humidities = [d.humidity for d in valid if d.humidity is not None]
    if humidities:
        result['avg_hum'] = sum(humidities) / len(humidities)
        result['hum_count'] = len(humidities)
    
    # Find most common normalized condition
    condition_counts: Dict[str, int] = {}
    for d in valid:
        normalized = normalize_condition(d.condition)
        if normalized:  # Skip empty conditions
            condition_counts[normalized] = condition_counts.get(normalized, 0) + 1
    
    if condition_counts:
        result['condition'] = max(condition_counts, key=condition_counts.get)
    else:
        result['condition'] = "Unknown"
    
    return result


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
    if code <= 77:
        return "Snowy"
    if code <= 86:
        return "Snowy"  # Snow showers
    if code <= 94:
        return "Rainy"  # Showers
    return "Stormy"  # Thunderstorm codes 95-99


# Unified condition mapping: Normalized name -> (keywords, emoji)
CONDITION_INFO = {
    'Clear': (['clear', 'sunny'], '☀️'),
    'Partly Cloudy': (['partly'], '⛅'),
    'Cloudy': (['cloud', 'overcast'], '☁️'),
    'Rainy': (['rain', 'drizzle'], '🌧️'),
    'Snowy': (['snow', 'sleet'], '❄️'),
    'Foggy': (['fog', 'mist'], '🌫️'),
    'Stormy': (['storm', 'thunder'], '⛈️'),
}


def normalize_condition(condition: str) -> str:
    """Normalize weather conditions to standard categories."""
    lower = condition.lower()
    for normalized, (keywords, _) in CONDITION_INFO.items():
        if any(kw in lower for kw in keywords):
            return normalized
    return condition


def get_condition_emoji(condition: str) -> str:
    """Map weather condition to emoji."""
    lower = condition.lower()
    
    # Check normalized names first
    for normalized, (keywords, emoji) in CONDITION_INFO.items():
        if normalized.lower() in lower:
            return emoji
        # Also check keywords
        if any(kw in lower for kw in keywords):
            return emoji
    
    return '🌡️'  # Default thermometer emoji
