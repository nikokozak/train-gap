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

One event per line. `t_ms` is milliseconds since the session started. The clock keeps counting while the phone sleeps, so time the app did not run shows as a gap in the data.
- `session_start`: route, start station, the stations in ride order (`route_stations`), carrier, probe URLs, motion rate
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

JSON output (segments and stops with times and probe statistics):

```bash
go run ./cmd/recording-summarize --json -- path/to/session.ndjson
```

### How stops are found

For current recordings, the summarizer finds the stops in the motion trace:
1) Each one-second motion batch becomes one number: RMS acceleration (milli-g).
2) A 5-second rolling median removes single jolts.
3) Each second is **still**, **moving** (train), or **walking**. Motion after the Stop tap is ignored.
4) Moving runs are joined across short pauses. Runs shorter than a ride between stations are dropped.
5) The start and end stations give the expected number of segments.

The result is in `stop_detection.status`:
- `matched`: the count is correct. Segments and stops are labelled.
- `matched_after_removing_holds`: correct after removing stops with no cell service. Underground stations have service, so such a stop is probably a train held in a tunnel.
- `unverified_end`: you chose "Not sure". Labels count forward from the start station.
- `count_mismatch`: no labels. Review the stops, or tune the thresholds.
- `no_motion`: the recording has no motion data.

### Tuning the thresholds (calibration ride)

The default thresholds are estimates. On a calibration ride, tap **Mark** at every stop, then:

```bash
go run ./cmd/recording-summarize -energy -- path/to/session.ndjson > energy.csv
```

Each row is one second: RMS, smoothed RMS, state, and `mark=1` where you tapped Mark. Rows with a mark must be `still`. Set the flags from what you see, and check that the summary reports every mark inside a detected stop:

```bash
go run ./cmd/recording-summarize -moving-mg 30 -walking-mg 250 -min-stop-s 8 -min-segment-s 20 -- path/to/session.ndjson
```

## Next processing step (how Train-Gap can use it)

The labelled segments and stops, with their probe statistics, become a timeline that the simulator replays: per segment, the share of successful probes and the round-trip times of the successful ones.

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
