# Fast — CLI Speed Test

Fast is a lightweight utility for checking internet speed directly in the terminal. The program uses the nearest Netflix Open Connect servers (via the fast.com API) and displays:
- download speed in Mbps/Gbps;
- upload speed in Mbps/Gbps;
- TCP connect time (ping): idle and loaded;
- DNS resolution time;
- a mini-graph (sparkline) of speed changes and peak value.

Demonstration: (demo.gif in repository)

## Features
- Interactive TUI powered by [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) and [lipgloss](https://github.com/charmbracelet/lipgloss).
- Parallel download/upload streams to saturate the channel.
- Separate ping metrics: idle ping (before download) and loaded ping (during traffic); DNS query time is measured using a unique subdomain to bypass local cache.
- Braille sparkline for compact representation of the speed time series.
- Simple, static binary utility — requires no external services other than internet access.

## Source Code Structure
- [main.go](file:///home/user/fast/main.go) — entry point: fetches targets list, runs the TUI model, updates and visualizes metrics.
- [fast.go](file:///home/user/fast/fast.go) — network logic: retrieves fast.com token, queries targets, implements download/upload workers, counts bytes, measures ping and DNS.
- [sparkline.go](file:///home/user/fast/sparkline.go) — renders sparklines using Braille characters.
- Configuration constants that can be tweaked in the code: `connections` (number of parallel connections), `duration` (duration of download/upload phases), `sparkWidth` (sparkline width).

## Installation & Compilation
It is recommended to build from source (this fork is stored in the current repository):
```bash
git clone https://github.com/dumpe2fs/fast.git
cd fast
go build -o fast .
```
After compilation, run:
```bash
./fast
```

> [!NOTE]
> Note regarding `go install`: the original upstream module in `go.mod` is declared as `github.com/maaslalani/fast`. If you want to install from this fork using `go install`, build it manually (`git clone` + `go build`) or update the module path in `go.mod` before installation.

### Requirements
- Go 1.26+ (go.mod specifies 1.26.3)
- Internet access (HTTPS)

## Usage
Just run `./fast`. The interface updates in real-time:
- "Download", "Upload", "Ping", and "DNS Time" lines are displayed.
- The sparkline shows speed changes during the current phase.
- Key bindings: `q`, `Esc`, or `Ctrl+C` to exit.

## How Metrics are Measured
- **Download**: parallel GET requests to URLs from the fast.com API; byte counter is updated atomically, speed is calculated on ticks.
- **Upload**: POST requests with a zero-byte stream generator; measures the sent volume.
- **Ping**: establishes a TCP connection to the IP address of the target host (resolved via LookupIP) — this isolates TCP-RTT from DNS.
- **DNS**: resolves a unique domain name (using a timestamp) to measure the actual resolution time without local cache interference.

## Configuration
To change the program behavior (number of streams, phase duration, etc.), edit the constants in `main.go`:
- `connections` — number of parallel connections per target;
- `duration` — duration of the download or upload phase;
- `sparkWidth` — sparkline graph width.

If you want to add CLI flags (for example, to change duration via flags), you can extend `main.go` using the standard `flag` package.

## Known Limitations
- The program relies on the fast.com API and Netflix Open Connect. If fast.com changes token generation or its API, regex or token retrieval logic (in `fast.go`) might need an update.
- Metrics are reported in megabits/gigabits per second, similar to fast.com; short peak values are displayed separately.
- Old installation instructions via `go install github.com/maaslalani/fast@main` from the original repository may not apply to this fork — build from source for safety.

## License and Feedback
The project is licensed under the MIT License (see [LICENSE](file:///home/user/fast/LICENSE)).

If you want to report a bug or suggest an improvement:
- Open an issue in the repository: https://github.com/dumpe2fs/fast/issues
- For quick fixes, create a PR with your changes.

Thank you for using Fast!
