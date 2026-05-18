# Larakube

Larakube is a terminal UI for inspecting Laravel workloads running in Kubernetes.

It is built for the common production debugging loop:

1. Pick a Laravel namespace.
2. Select one or more pods.
3. Run Laravel-specific inspection commands.
4. Read logs or command output without leaving the terminal.

Larakube intentionally stays small and shells out to `kubectl` instead of using the Kubernetes Go SDK.

## Why

Laravel applications in Kubernetes often need the same checks over and over:

- Which pods are healthy?
- What environment is this app running in?
- Are there failed queue jobs?
- What do the latest logs look like?
- Is Horizon running?
- What routes or scheduled tasks are registered?

Larakube puts those checks behind a single TUI and lets you run them across multiple pods in the same namespace.

## Features

- Config-driven namespace picker
- Multi-select pod workflow
- Laravel-oriented action menu
- Aggregated output across selected pods
- Configurable logging source:
  - Kubernetes stdout with `kubectl logs`
  - log file tailing inside the container
- Horizontal and vertical scrolling for long output
- Optional feature-gated actions such as Horizon
- Custom Artisan commands
- Custom shell commands
- Interactive config bootstrap with `go run . init`

## Built-In Actions

- Failed queue jobs
- Laravel version
- App environment
- Route list
- Schedule list
- Config overview
- Migration status
- Queue status
- Optimize clear with confirmation
- Horizon status when enabled
- Application log tailing
- Custom Artisan command
- Custom shell command

## Requirements

- Go 1.22+
- `kubectl` installed and available in `PATH`
- A working kubeconfig / current context
- Access to the target namespaces and pods
- PHP / Laravel available inside the target application containers

## Installation

### Run from source

```bash
go run .
```

### Build a local binary

```bash
go build -o larakube .
./larakube
```

### Download a release binary

Prebuilt binaries are published on the GitHub Releases page for tagged versions:

`https://github.com/parsingeye/larakube/releases`

## Quick Start

### 1. Create a config interactively

```bash
go run . init
```

This writes a starter `larakube.config.json` in the project directory.

### 2. Or create the config manually

```json
{
  "laravelNamespaces": [
    "production-web",
    "production-worker"
  ],
  "features": {
    "horizon": false
  },
  "logs": {
    "source": "stdout",
    "path": "storage/logs/laravel.log",
    "tail": 100,
    "container": ""
  }
}
```

### 3. Run the app

```bash
./larakube
```

## Configuration

Larakube reads `larakube.config.json` from the project root.

### `laravelNamespaces`

The namespace picker only shows namespaces listed here.

Example:

```json
{
  "laravelNamespaces": ["production-web", "production-worker"]
}
```

### `features`

Feature flags control optional actions.

Supported flags:

- `horizon`: show Horizon-related actions

Example:

```json
{
  "features": {
    "horizon": true
  }
}
```

### `logs`

Controls how the log action works.

Supported fields:

- `source`: `"stdout"` or `"file"`
- `path`: file path when `source` is `"file"`
- `tail`: number of lines to read
- `container`: optional container name when using `stdout`

Examples:

Kubernetes stdout logging:

```json
{
  "logs": {
    "source": "stdout",
    "tail": 200,
    "container": "app"
  }
}
```

Container file logging:

```json
{
  "logs": {
    "source": "file",
    "path": "storage/logs/laravel.log",
    "tail": 200
  }
}
```

### Defaults

If omitted, Larakube uses these defaults:

- `logs.source`: `stdout`
- `logs.path`: `storage/logs/laravel.log`
- `logs.tail`: `100`

## Usage

### Namespace Selection

- `j` / `k`: move
- `pgup` / `pgdn`: page
- `enter`: select namespace
- `r`: reload namespaces
- `q`: quit

### Pod Selection

- `j` / `k`: move
- `pgup` / `pgdn`: page
- `space`: select pod
- `enter`: open actions
- `l`: toggle Laravel-only filtering
- `r`: refresh pods
- `esc`: back to namespaces

### Action Menu

- `j` / `k`: move
- `pgup` / `pgdn`: page
- `enter`: run action
- `esc`: back

### Output View

- `j` / `k`: vertical scroll
- `h` / `l`: horizontal scroll
- `pgup` / `pgdn`: page vertically
- `r`: rerun current action
- `esc`: back

## Command Behavior

- Pod discovery uses `kubectl get pods -n <namespace> -o json`
- Commands run with `kubectl exec -n <namespace> <pod> -- ...`
- Stdout log mode uses `kubectl logs -n <namespace> <pod> ...`
- `kubectl` calls use a timeout to avoid hanging forever
- Output is normalized before rendering
- Multi-pod results are prefixed with `=== namespace/pod ===`

## Notes

- Namespace selection is intentionally config-driven.
- The current version targets the default pod container unless `logs.container` is set for stdout logs.
- Pods in configured namespaces are treated as Laravel pods even when their names are generic.
- Some Laravel commands may still be expensive in large production datasets.
- `queue:failed` is run with unlimited PHP memory to avoid common memory exhaustion failures in large failed-job tables.

## Development

Run locally:

```bash
go run .
```

Create a config:

```bash
go run . init
```

Build:

```bash
go build .
```

Format:

```bash
gofmt -w *.go
```

## Releases

GitHub releases are built automatically with GitHub Actions when you push a version tag.

Example:

```bash
git tag v0.1.0
git push origin v0.1.0
```

That workflow builds archives for:

- macOS amd64
- macOS arm64
- Linux amd64
- Linux arm64

Each release archive includes:

- `larakube`
- `README.md`
- `LICENSE`

## Roadmap

- Container selection for multi-container pods
- Live log streaming
- Per-namespace overrides
- Additional Laravel diagnostics
- Exportable output

## License

MIT
