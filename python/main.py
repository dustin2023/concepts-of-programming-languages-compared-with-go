#!/usr/bin/env python3
"""Weather Data Aggregator - Async API fetching demo."""

import argparse
import asyncio
import re
import sys
import time
from typing import List, Optional

from dotenv import load_dotenv

from weather import (
    WeatherData,
    fetch_weather_concurrently,
    fetch_weather_sequentially,
    aggregate_weather,
    get_condition_emoji,
    init_sources,
    normalize_source,
    load_weather_codes,
)


def validate_city_name(city: str) -> Optional[str]:
    """Validate city name (letters, spaces, hyphens, periods, max 100 chars)."""
    city = city.strip()

    if not city:
        return None

    if len(city) > 100:
        return None

    # Allow Unicode letters (including umlauts, accents), numbers, spaces, hyphens, apostrophes, and periods
    if not re.match(r"^[\w\s\-'\.]+$", city, re.UNICODE):
        return None

    return city


def create_parser() -> argparse.ArgumentParser:
    """Create and return the argument parser."""
    parser = argparse.ArgumentParser(
        description="Weather Data Aggregator",
        epilog='Examples: %(prog)s --city New York   |   %(prog)s --city "O\'Brien"',
    )
    parser.add_argument(
        "--city",
        nargs="+",
        required=True,
        metavar="NAME",
        help="City name (spaces allowed, quotes optional)",
    )
    parser.add_argument("--sequential", action="store_true", help="Sequential fetching")
    parser.add_argument(
        "--exclude",
        nargs="+",
        default=[],
        help="Exclude sources (comma-separated; spaces allowed without quotes)",
    )
    return parser


def print_city_validation_error() -> None:
    """Print city validation error message and usage."""
    print(
        "Error: Invalid city name. Use letters (including ü, é, ñ), numbers, spaces, hyphens, apostrophes, and periods.",
        file=sys.stderr,
    )
    print("\nUsage: python main.py --city <city> [OPTIONS]")
    print("\nOptions:")
    print("  --city       City name (required, spaces allowed)")
    print("  --sequential Use sequential fetching instead of concurrent (optional)")
    print("  --exclude    Comma-separated source names to skip (optional)")
    print("\nExamples:")
    print("  ./main.py --city New York")
    print('  ./main.py --city "O\'Brien"   # apostrophe needs double-quotes')
    print("  ./main.py --city Berlin --exclude WeatherAPI.com")
    print("\nAPI keys are loaded from .env file.")


def display_results(data: List[WeatherData]) -> None:
    """Display per-source results and aggregated statistics."""
    for d in data:
        duration_str = f" ({d.duration_ms:.0f}ms)" if d.duration_ms else ""
        if d.error:
            print(f"❌ {d.source + ':':<18} ⚠️  {d.error}{duration_str}")
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
            print(f"→ Avg Humidity:     {agg['avg_hum']:.1f}%")
        else:
            print(f"→ Avg Humidity:     N/A (no sources)")
        print(f"→ Consensus:        {agg['condition']} {emoji}")
    else:
        print("→ No valid data available")


async def main() -> int:
    load_dotenv()

    # Load weather code mappings once (like Go, fail-fast if file missing)
    try:
        load_weather_codes()
    except Exception as e:
        print(f"Error loading weather codes: {e}", file=sys.stderr)
        return 1

    args = create_parser().parse_args()

    # Join multi-word city names
    city = validate_city_name(" ".join(args.city))
    if not city:
        print_city_validation_error()
        return 1

    sources = init_sources()

    # Filter out excluded sources
    if args.exclude:
        exclude_raw = " ".join(args.exclude)
        excluded_names = {
            normalize_source(name.strip()) for name in exclude_raw.split(",")
        }
        sources = [s for s in sources if normalize_source(s.name) not in excluded_names]

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
