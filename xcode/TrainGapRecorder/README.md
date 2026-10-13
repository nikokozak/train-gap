# TrainGapRecorder (iPhone utility)

Dev-only SwiftUI utility to record “real” connectivity while riding a subway.

## Xcode project

Open:
- `xcode/TrainGapRecorder/TrainGapRecorder.xcodeproj`

Key paths:
- App code: `xcode/TrainGapRecorder/TrainGapRecorder/`
- Route dataset (bundled): `TrainGapRecorder/lines.json`, a symlink to `internal/lines/lines.json`, which the simulator uses too

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

## Station dataset

The app and the simulator share `internal/lines/lines.json`. To rebuild it, see "Station dataset" in `docs/recordings.md`.
