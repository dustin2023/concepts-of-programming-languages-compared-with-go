#!/usr/bin/env python3
"""
Weather Data Aggregator - CLI Entry Point
Demonstrates Python's asyncio for concurrent API calls.

Usage:
    python main.py --city Munich
    python main.py --city Berlin --sequential

Semester Project: Parallel Programming - Go & Python
"""

import argparse
import asyncio
import os
import sys
import time
from typing import List

from dotenv import load_dotenv

from weather import (
    WeatherData,
    OpenMeteoSource,
    WttrinSource,
    WeatherAPISource,
    WeatherstackSource,
    MeteosourceSource,
    PirateWeatherSource,
    fetch_weather_concurrently,
    fetch_weather_sequentially,
    aggregate_weather,
    get_condition_emoji,
)


def init_sources() -> list:
    """
    Initialize all available weather sources.
    Free sources are always included.
    API-key sources are added if keys exist in environment.
    """
    # Always include free sources
    sources = [
        OpenMeteoSource(),
        WttrinSource(),
    ]

    # Add API-key sources if keys are available
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

    # Individual results
    for d in data:
        if d.error:
            print(f"❌ {d.source + ':':<18} ERROR: {d.error}")
        else:
            hum_str = f"{d.humidity:.0f}%" if d.humidity is not None else "N/A"
            print(
                f"✅ {d.source + ':':<18} {d.temperature:.1f}°C, {hum_str} humidity, {d.condition}"
            )

    # Aggregated results
    agg = aggregate_weather(data)
    emoji = get_condition_emoji(agg["condition"])

    print(f"\n📊 Aggregated ({agg['valid_count']}/{len(data)} valid):")

    if agg["valid_count"] > 0:
        print(f"→ Avg Temperature: {agg['avg_temp']:.2f}°C")
        print(f"→ Avg Humidity:    {agg['avg_hum']:.1f}%")
        print(f"→ Consensus:       {agg['condition']} {emoji}")
    else:
        print("→ No valid data available")


async def main() -> int:
    """Main entry point."""

    load_dotenv()

    # Parse command-line arguments
    parser = argparse.ArgumentParser(
        description="Weather Data Aggregator - Fetches weather from multiple APIs",
        formatter_class=argparse.RawDescriptionHelpFormatter,
        epilog="""
        Examples:
            python main.py --city Munich
            python main.py --city "New York" --sequential
        """,
    )
    parser.add_argument("--city", required=True, help="City name (required)")
    parser.add_argument(
        "--sequential",
        action="store_true",
        help="Use sequential fetching for performance comparison",
    )

    args = parser.parse_args()

    # Validate city input
    city = args.city.strip()
    if not city:
        print("Error: City name is required and cannot be empty", file=sys.stderr)
        return 1

    # Initialize sources
    sources = init_sources()
    print(f"🌍 {city} | Fetching from {len(sources)} sources...")

    # Fetch weather data
    start_time = time.perf_counter()

    if args.sequential:
        data = await fetch_weather_sequentially(city, sources)
    else:
        data = await fetch_weather_concurrently(city, sources)

    duration = time.perf_counter() - start_time
    print(f"⏱️  Completed in {duration:.3f}s\n")

    # Display results
    display_results(data)

    return 0


if __name__ == "__main__":
    sys.exit(asyncio.run(main()))
