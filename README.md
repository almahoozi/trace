# trace

TUI to inspect Grafana traces and logs.

## Installation

```bash
go install github.com/almahoozi/trace/cmd/t@latest
```

## Configuration

Open and edit configuration:

```bash
t config
```

Import an existing config file:

```bash
t config import config.json
```

Export the current config to a file for backup or sharing:

```bash
t config export config.json
```

## Usage

```bash
t <trace-id>
```

> [!NOTE]
> On first run, if not configured yet, the CLI prompts you to enter the Grafana
> base URL and/or token. The token is stored in the OS keyring for secure access.
> The base URL is stored in a config file.
