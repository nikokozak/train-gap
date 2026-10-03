# Recordings (real subway measurements)

The iPhone utility (`xcode/TrainGapRecorder/`) records NDJSON events matching:
- `schemas/recording.v1.schema.json`

The recorder is the source of truth for route selection. Each recording includes the line/station *names* in `session_start`, so processing does not need to share the same station list.

## Workflow

The recorder records evidence, not station labels. You do not mark stations during the ride, so you cannot forget a mark. After the ride, `recording-summarize` finds the stops in the motion trace and labels them from your start and end stations.

1) Choose **Line**, **Direction**, **Starting station**, and **Carrier**. The app remembers them for the next ride.
2) On the platform, tap **Start recording**.
3) Lock the phone and put it away. Recording continues while the phone is locked.
4) Optional: tap **Mark** when the doors open. A mark is a timestamp only. It helps to check the stop detection.
5) When you get off, tap **Stop**, then choose the station where you got off. **Not sure** is a valid answer.

Before the first ride:
- Allow location access when the app asks. It keeps the app running while the phone is locked. The app does not save your location. Without it, keep the app open with the screen on.
- Turn Wi-Fi off. Otherwise you measure station Wi-Fi, not cellular service. The app shows a warning when Wi-Fi is on.

## Export from iPhone

In the recorder app:
1) Record a session
2) Open **Sessions**
3) Share the `.ndjson` file (AirDrop / Files / etc.)

## What’s in the NDJSON

One event per line. `t_ms` is milliseconds since the session started.
- `session_start`: route, start station, carrier, probe URLs, motion rate
- `sample`: one network probe per second, plus network path and radio type (LTE, NR)
- `motion`: one second of accelerometer readings (20 Hz, milli-g, gravity removed)
- `mark`: the rider tapped Mark
- `session_end`: Stop time, end station (absent for "Not sure"), and a summary

Recordings from before 2026-10 contain `station_depart`, `station_arrive`, and `undo` markers instead of `motion` and `mark`.

### Probe semantics (rate-limit friendly)

Each `sample` includes an HTTP probe:
- A probe starts every second on a fixed clock. A slow probe does not delay the next one, so probes can overlap and samples can appear out of `t_ms` order.
- A **small probe set** (multiple URLs) is used and rotated round-robin.
- If we get *any* HTTP response, `probe.ok=true` (even non-2xx).
- If an endpoint returns **HTTP 429**, it is recorded as `probe.error="rate_limited"` and that URL is temporarily backed off.
- Only transport failures and timeouts (4 s) result in `probe.ok=false`.
- `reused_connection=false` means `rtt_ms` includes DNS, TCP, and TLS setup, for example the first probe after a blackout.

## Summarize a recording

```bash
go run ./cmd/recording-summarize -- path/to/session.ndjson
```

JSON output:

```bash
go run ./cmd/recording-summarize --json -- path/to/session.ndjson
```

## Next processing step (how Train-Gap can use it)

The most direct mapping into Train-Gap is:
- derive an empirical “blackout” probability and RTT distribution per segment/time window
- use it to tune `internal/sim/train.go` (packet loss + latency profiles)

Once stops are labelled (from the motion trace and the start and end stations), samples can be grouped by segment to make per-segment connectivity profiles. Stop detection is not built yet; until then, `recording-summarize` reports segments only for recordings with the old depart/arrive markers.

## Keeping the Go simulator in sync with the iPhone route dataset (optional)

The iPhone app bundles `xcode/TrainGapRecorder/TrainGapRecorder/lines.json`. If you want the Go simulator to use the exact same dataset:

```bash
go run ./cmd/lines-sync
```

This writes `internal/lines/lines.json`, which `internal/lines` loads at runtime.

## Generating a full NYC dataset (GTFS import)

If you want the full station list for major services (A/C/E, 1–7, N/Q/R/W, etc.), import from the MTA GTFS zip:

```bash
go run ./cmd/lines-gtfs -in /path/to/google_transit.zip
```

By default this overwrites the recorder’s bundled `lines.json` at:
- `xcode/TrainGapRecorder/TrainGapRecorder/lines.json`
