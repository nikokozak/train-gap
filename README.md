# Train-Gap

Train-Gap is a local proxy that replays real NYC subway rides. Each ride was recorded on a phone: the cell service and the delay in every second. While the recorded train is in a tunnel without service, your app's connections stall, as they did on the real train. When the train gets to the next station, they continue.

Use it to see what your app does on a commute: spinners that never end, retries that pile up, timeouts that are too short or too long, offline queues that never flush.

![A 3 train ride. In the tunnel, new connections wait for service; at the next station they finish.](docs/demo.gif)

## Install

```bash
go install github.com/nikokozak/train-gap@latest
```

This needs Go 1.25 or later. The result is one binary with the rides built in.

## Use

**Watch a ride.** Start the terminal view, then send traffic through the proxy:

```bash
train-gap                      # a random ride; proxy on localhost:1080
curl -x http://localhost:1080 https://example.com
```

The port speaks HTTP (`http://localhost:1080`) and SOCKS5 (`socks5://localhost:1080`). The view shows the train on a strip map, the signal, the delay the proxy adds, and each connection through the proxy, with the time it waited for service.

Keys: `tab` choose a ride, `space` pause, `n` next stop or run, `←` `→` speed, `r` real time, `q` quit.

**Run a command on a ride.** `run` starts the proxy on a free port, points the command at it, and plays the ride while the command runs:

```bash
train-gap run --speed 4 -- npm test
train-gap run --line 3 -- python3 sync.py
```

`run` sets `HTTP_PROXY`, `HTTPS_PROXY`, and `ALL_PROXY` (both cases), and `NODE_USE_ENV_PROXY=1` so that Node's `fetch` uses them (Node 22.21+ or 24.5+). This covers curl, Go, Python (`requests`, `urllib`), pip, npm, and git. On stderr, it writes a line for each stop and each change of service, so you can match a failure in your output to the moment it happened. The exit code is the command's exit code.

**Choose a ride.** Without flags, Train-Gap picks a random ride. `train-gap rides` lists them.

```bash
train-gap --line 3                       # a random ride on the 3
train-gap --ride <name>                  # a ride from `train-gap rides`
train-gap --ride session.ndjson          # your own recording from the iPhone recorder
```

Flags for both modes: `--line`, `--ride`, `--speed` (0.1 to 10, 1 is real time), `--manual` (start paused), `--port`.

## How it works

A ride is a list of station stops and runs between stations, with one measurement for each second: either no service, or the delay that the proxy adds in each direction (half the round trip that the phone measured). Train-Gap plays the ride there and back, and then again. For the way back, it uses a recorded ride in the other direction between the same stations; if there is none, it plays the ride backward.

Without service, nothing fails and nothing is dropped. Reads, writes, and new connections wait until service comes back, as TCP does in a tunnel. DNS lookups that the proxy does (for HTTP clients, and SOCKS5 clients that send host names) wait too. Your app's own timeouts decide what fails.

## Rides

The rides are in [`rides/`](rides/), one small JSON file each. There are few so far; more come as people ride and record. To record a ride and add it, see [docs/recordings.md](docs/recordings.md). A ride file keeps the line, the stations, the month, the carrier, and the measurements. It leaves out the day and time, your device, and your location.

## Compared with other tools

- **Toxiproxy** injects faults that you configure, for one upstream at a time. Train-Gap is a forward proxy for all of an app's traffic, and the faults come from a real ride.
- **Network Link Conditioner** (macOS) and **tc netem** (Linux) shape the whole machine with a fixed profile. Train-Gap changes over time, affects only the traffic that you send to it, and changes no system settings.

## Limits

- Only lines and stations that someone recorded.
- Only TCP. UDP, for example HTTP/3, does not go through the proxy.
- Only clients that use a proxy. Go's HTTP client never proxies `localhost`, and most clients skip hosts in `NO_PROXY`.
- The delay applies to each chunk of data that the proxy reads or writes, not to each packet.

## License

MIT. See [LICENSE](LICENSE).
