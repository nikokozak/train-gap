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

Your start and end stations give the number of segments. The summarizer then finds where the stops are, using two kinds of evidence:
- **Motion.** A running train makes the phone jolt; a stopped train does not. The jolt is measured relative to the ride's own typical level, so it does not matter whether the phone is in your hand or your pocket.
- **Service.** Stations have cell service. On lines without tunnel coverage, each tunnel shows as one long stretch of failed probes. A quiet stretch without service is a train crawling in a tunnel, not a stop.

It chooses the stops that fit both best over the whole ride. Motion after the Stop tap is ignored.

The result is in `stop_detection.status`:
- `matched`: the number of tunnels without service equals the number of segments. Each segment contains one tunnel; motion places the stops. This is the reliable case.
- `motion_only`: the tunnels did not match the count (a tunnel with service, elevated track, or an express train passing stations). Motion and service place the stops. Check the stop list.
- `unverified_end`: you chose "Not sure". The tunnel count gives the number of segments.
- `count_unknown`: "Not sure", and the tunnels do not give a count. No labels.
- `no_fit`: no split fits the time limits. Check the end station.
- `no_motion`: the recording has no motion data.

### Checking and tuning (calibration ride)

On a calibration ride, tap **Mark** when the doors open at each station. The summary then reports how many marks fall in detected stops ("marks near detected stops"). A mark is near a stop when it is within 3 s of it. If you walk off the train as the doors open, that last mark will not count.

To see the evidence second by second:

```bash
go run ./cmd/recording-summarize -energy -- path/to/session.ndjson > energy.csv
```

Each row is one second: the jolt, the smoothed jolt, the jolt relative to the ride's typical level (stops are well below 1), service (`1` a probe got through, `0` probes failed), and `mark=1` where you tapped Mark. The flags `-min-stop-s`, `-min-segment-s`, `-smooth-s`, and `-tunnel-gap-s` change the limits.

## Ride your recording

```bash
train-gap --ride path/to/session.ndjson
train-gap run --ride path/to/session.ndjson -- npm test
```

Train-Gap finds the stops as above and plays the ride as it was recorded. Each second gets the result of the probe that started in it:
- A failed probe is a second without service. Connections stall through it.
- A successful probe sets the added delay to half its round-trip time. A probe on a new connection includes TCP and TLS setup, so it counts as three round trips.

The stop detection status must be `matched`, `motion_only`, or `unverified_end`.

## Add your ride to Train-Gap

Rides in [`rides/`](../rides/) ship with Train-Gap. To add yours:

1) Export it:

   ```bash
   go run ./cmd/ride-export path/to/session.ndjson
   ```

   This writes `rides/<line>-<from>-to-<to>-<month>-<id>.json`. If stop detection was not `matched`, the tool says so; check the stops with `recording-summarize` first.
2) Try it: `go run . --ride <name>`.
3) Open a pull request with the new file. `go test ./rides` checks that it plays on its line.

A ride file keeps the line, the stations, the stops and runs, the month, the carrier, and one value for each second: the delay in milliseconds, or `-1` for no service. It leaves out the day and time, the device, the session ID, the probe URLs, and the motion data. A route that you ride every day still shows roughly where you live or work; share only the rides that you are comfortable with.

## Station dataset

One file, `internal/lines/lines.json`, serves both apps. The simulator embeds it; the Xcode project links to it (`xcode/TrainGapRecorder/TrainGapRecorder/lines.json` is a symlink). Station IDs are GTFS stop IDs, so recordings and the simulator use the same IDs.

To rebuild it from the [MTA GTFS zip](https://new.mta.info/developers):

```bash
go run ./cmd/lines-gtfs -in /path/to/google_transit.zip -replace-existing
```

Rides refer to stations by these IDs, and each run must join two neighbouring stations of the line. After a rebuild, `go test ./rides` checks that every ride still fits.
