#!/usr/bin/env python3
"""Weather Data Aggregator - CLI Entry Point

Demonstrates Python's asyncio for concurrent API calls.
Compares sequential vs concurrent performance.
"""

import argparse
import asyncio
import os
import re
import sys
import time
from typing import List, Optional

from dotenv import load_dotenv

from weather import (
    WeatherData,
    OpenMeteoSource,
    TomorrowIOSource,
    WeatherAPISource,
    WeatherstackSource,
    MeteosourceSource,
    PirateWeatherSource,
    fetch_weather_concurrently,
    fetch_weather_sequentially,
    aggregate_weather,
    get_condition_emoji,
    MAX_CITY_NAME_LENGTH,
)

# Error message formatting for better UX
ERROR_MESSAGES = {
    "timeout": "⏱️  Request timeout",
    "HTTP 400": "⚠️  Bad request",
    "HTTP 429": "🚫 Rate limit exceeded",
    "HTTP 401": "🔑 Authentication failed",
    "HTTP 403": "🔒 Access forbidden",
    "HTTP 404": "❓ Resource not found",
    "HTTP 500": "⚠️  Server error",
    "HTTP 503": "🔧 Service unavailable",
    "network error": "🌐 Network connection failed",
    "invalid JSON": "📄 Invalid response format",
    "city not found": "🗺️  City not found",
}


def format_error(error: str) -> str:
    """Format error message with emoji and human-readable text."""
    for key, message in ERROR_MESSAGES.items():
        if key in error:
            return message
    return f"⚠️  {error}"


def validate_city_name(city: str) -> Optional[str]:
    """Validate and sanitize city name input."""
    city = city.strip()

    if not city:
        return None

    if len(city) > MAX_CITY_NAME_LENGTH:
        return None

    # Allow letters, spaces, hyphens, apostrophes, periods
    if not re.match(r"^[a-zA-Z\s\-'\.]+$", city):
        return None

    return city


def init_sources() -> list:
    """Initialize all available weather sources."""
    sources = [
        OpenMeteoSource(),
    ]

    # Add API-key sources if keys are available
    if key := os.getenv("TOMORROW_API_KEY"):
        sources.append(TomorrowIOSource(key))

    if key := os.getenv("WEATHER_API_COM_KEY"):
        sources.append(WeatherAPISource(key))

    if key := os.getenv("WEATHERSTACK_API_KEY"):
        sources.append(WeatherstackSource(key))

    if key := os.getenv("METEOSOURCE_API_KEY"):
        sources.append(MeteosourceSource(key))

    if key := os.getenv("PIRATE_WEATHER_API_KEY"):
        sources.append(PirateWeatherSource(key))

    return sources


def display_results(data: List[WeatherData]) -> None:
    """Display individual results and aggregated summary."""
    for d in data:
        duration_str = f" ({d.duration_ms:.0f}ms)" if d.duration_ms else ""
        if d.error:
            error_msg = format_error(d.error)
            print(f"❌ {d.source + ':':<18} {error_msg}{duration_str}")
        else:
            hum_str = f"{d.humidity:.0f}%" if d.humidity is not None else "N/A"
            print(
                f"✅ {d.source + ':':<18} {d.temperature:.1f}°C, {hum_str} humidity, {d.condition}{duration_str}"
            )

    agg = aggregate_weather(data)
    emoji = get_condition_emoji(agg["condition"])

    print(f"\n📊 Aggregated ({agg['valid_count']}/{len(data)} valid):")

    if agg["valid_count"] > 0:
        print(f"→ Avg Temperature: {agg['avg_temp']:.2f}°C")

        if agg["hum_count"] > 0:
            print(f"→ Avg Humidity:{agg['avg_hum']:.1f}%")
        else:
            print(f"→ Avg Humidity: N/A (no sources)")

        print(f"→ Consensus: {agg['condition']} {emoji}")
    else:
        print("→ No valid data available")


async def main() -> int:
    load_dotenv()

    parser = argparse.ArgumentParser(
        description="Weather Data Aggregator - Fetches weather from multiple APIs",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="Examples:\n  python main.py --city Munich\n  python main.py --city 'New York' --sequential",
    )
    parser.add_argument("--city", required=True, help="City name (required)")
    parser.add_argument(
        "--sequential", action="store_true", help="Use sequential fetching"
    )
    parser.add_argument(
        "--exclude",
        default="",
        help="Comma-separated source names to exclude (e.g., 'wttr.in,WeatherAPI.com')",
    )

    args = parser.parse_args()

    city = validate_city_name(args.city)
    if not city:
        print(
            "Error: Invalid city name. Use only letters, spaces, hyphens, and periods.",
            file=sys.stderr,
        )
        print(
            "\nUsage: weather-aggregator --city=<city> [--sequential] [--exclude=source1,source2]"
        )
        print("  --city       City name (required)")
        print("  --sequential Use sequential fetching instead of concurrent (optional)")
        print("  --exclude    Comma-separated source names to skip (optional)")
        print("\nAPI keys are loaded from .env file.")
        print("Free sources: Open-Meteo")
        return 1

    sources = init_sources()

    # Filter out excluded sources
    if args.exclude:
        excluded_names = {name.strip() for name in args.exclude.split(",")}
        sources = [s for s in sources if s.name not in excluded_names]

    if not sources:
        print("Error: All sources were excluded", file=sys.stderr)
        return 1

    print(f"🌍 {city} | Fetching from {len(sources)} sources...")

    start_time = time.perf_counter()
    data = await (
        fetch_weather_sequentially if args.sequential else fetch_weather_concurrently
    )(city, sources)
    duration = time.perf_counter() - start_time

    print(f"⏱️  Completed in {duration:.3f}s\n")
    display_results(data)

    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
