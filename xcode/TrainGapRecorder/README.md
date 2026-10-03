# TrainGapRecorder (iPhone utility)

Dev-only SwiftUI utility to record “real” connectivity while riding a subway.

## Xcode project

Open:
- `xcode/TrainGapRecorder/TrainGapRecorder.xcodeproj`

Key paths:
- App code: `xcode/TrainGapRecorder/TrainGapRecorder/`
- Route dataset (bundled): `xcode/TrainGapRecorder/TrainGapRecorder/lines.json`

## Run on your iPhone

1) Connect your iPhone (unlock + “Trust” if prompted)
2) iPhone: Settings → Privacy & Security → **Developer Mode** → On
3) Xcode: Project → Signing & Capabilities
   - Team: your paid Apple Developer team
   - Bundle Identifier: unique (e.g. `com.yourname.TrainGapRecorder`)
4) Select your iPhone as the run destination
5) Press **Run** (▶)

If iOS prompts “Untrusted Developer” after install:
- iPhone: Settings → General → VPN & Device Management → trust the developer app/certificate.

## Export recordings

In the app: **Sessions** → share the `.ndjson` (AirDrop / Files / etc.)

Summarize:

```bash
go run ./cmd/recording-summarize -- path/to/session.ndjson
```

## Generate a full NYC station dataset (GTFS import)

Download the MTA Subway GTFS zip (`google_transit.zip`) and run:

```bash
go run ./cmd/lines-gtfs -in /path/to/google_transit.zip -replace-existing
```

This overwrites the app’s bundled `lines.json`.

Notes:
- The importer prefers a **weekday/daytime** stop pattern and tries to avoid “late-night local” or one-off reroutes that can add stations your train doesn’t actually stop at.
- If you need a different profile, see `go run ./cmd/lines-gtfs -h` (e.g. `-weekday-only=false` or a wider `-time-start/-time-end` window).

If you want the Go simulator to use the exact same dataset:

```bash
go run ./cmd/lines-sync
```
