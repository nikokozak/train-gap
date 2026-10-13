# Train-Gap

Train-Gap is a local proxy that rides an NYC subway line. While the train is in a tunnel without cell service, your app's connections stall, as they do on a real train. When the train gets to the next station, they continue.

Use it to see what your app does on a commute: spinners that never end, retries that pile up, timeouts that are too short or too long, offline queues that never flush.

![The L train leaves 8 Av. In the tunnel, new connections wait for service; at 6 Av they finish.](docs/demo.gif)

## Install

```bash
go install github.com/nikokozak/train-gap@latest
```

This needs Go 1.25 or later. The result is one binary with the station data built in.

## Use

**Watch the ride.** Start the terminal view, then send traffic through the proxy:

```bash
train-gap                      # the L train; proxy on localhost:1080
curl -x http://localhost:1080 https://example.com
```

The port speaks HTTP (`http://localhost:1080`) and SOCKS5 (`socks5://localhost:1080`). The view shows the train on a strip map, the signal, the delay the proxy adds, and each connection through the proxy, with the time it waited for service.

Keys: `space` pause, `n` next stop or run, `←` `→` speed, `r` real time, `q` quit.

**Run a command on the train.** `run` starts the proxy on a free port, points the command at it, and plays the ride while the command runs:

```bash
train-gap run --speed 4 -- npm test
train-gap run --line 3 -- python3 sync.py
```

`run` sets `HTTP_PROXY`, `HTTPS_PROXY`, and `ALL_PROXY` (both cases), and `NODE_USE_ENV_PROXY=1` so that Node's `fetch` uses them (Node 22.21+ or 24.5+). This covers curl, Go, Python (`requests`, `urllib`), pip, npm, and git. On stderr, it writes a line for each stop and each change of service, so you can match a failure in your output to the moment it happened. The exit code is the command's exit code.

**Replay a recorded ride.** The [iPhone recorder](xcode/TrainGapRecorder/) measures real service on a ride. Train-Gap can play that ride second by second:

```bash
train-gap --replay ride.ndjson
train-gap run --replay ride.ndjson -- npm test
```

See [docs/recordings.md](docs/recordings.md).

Flags for both modes: `--line` (default `L`), `--speed` (0.1 to 10, 1 is real time), `--manual` (start paused), `--port`, `--replay`. Lines: 1–7, A–G, J, L, M, N, Q, R, S, W, Z.

## How it works

Each line is a round trip of station stops and runs between stations. The running times come from the MTA's schedule (GTFS), so at `--speed 1` a ride takes as long as the real one.

The network condition is a function of time:
- At a station, there is service.
- A run between two underground stations goes through a tunnel without service. Service stops 16 s after departure and comes back 24 s before arrival. For 5 s on each side, the signal is weak: each chunk of data waits 500 ms.
- Elsewhere, each chunk of data waits 40 ms in each direction.
- Runs with at least one station above ground have service. So does the L's tunnel under the East River (1 Av to Bedford Av), which is wired for AT&T, Verizon, and T-Mobile.

The 16 s, 24 s, and 5 s values are medians from two recorded rides on the 3 train in Brooklyn. Other lines use the same rules; they are a model, not a measurement. A replay uses the measured values instead.

Without service, nothing fails and nothing is dropped. Reads, writes, and new connections wait until service comes back, as TCP does in a tunnel. DNS lookups that the proxy does (for HTTP clients, and SOCKS5 clients that send host names) wait too. Your app's own timeouts decide what fails.

## Compared with other tools

- **Toxiproxy** injects faults that you configure, for one upstream at a time. Train-Gap is a forward proxy for all of an app's traffic, and the faults follow a ride.
- **Network Link Conditioner** (macOS) and **tc netem** (Linux) shape the whole machine with a fixed profile. Train-Gap changes over time, affects only the traffic that you send to it, and changes no system settings.

## Limits

- Only TCP. UDP, for example HTTP/3, does not go through the proxy.
- Only clients that use a proxy. Go's HTTP client never proxies `localhost`, and most clients skip hosts in `NO_PROXY`.
- The delay applies to each chunk of data that the proxy reads or writes, not to each packet.
