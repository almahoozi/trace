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

Browse/query an environment:

```bash
t <env>
```

In browse mode:

- Press `:` to open the query builder.

From the CLI, use `-q` for quick queries:

```bash
t <env> -q service.name=checkout -q status=error
```

Examples:

```bash
# implicit operation/name-style query
t prod -q checkout.CreateOrder

# relative window (last 30 minutes)
t prod -q service.name=checkout -d 30m

# explicit time range
t prod -q status=error -t 2026-07-16T10:00:00Z/2026-07-16T11:00:00Z

# local start time + duration
t prod -q service.name=checkout -t 2026-07-16T10:15:30 -d 45m

# local end time - duration
t prod -q service.name=checkout -t 2026-07-16T10:15:30 -d -45m

# multiple query clauses
t prod -q service.name=checkout -q span.http.method=POST
```

## Themes

Built-in themes: `dark` and `light`.

List and set themes:

```bash
t theme list
t theme set <name>
```

Export and import themes:

```bash
t theme export ./my-theme.json
t theme import ./my-theme.json --name my-theme
```

You can also override the active theme via environment variable:

```bash
TRACE_THEME=light t <trace-id>
```

Get key help while tuning a theme:

```bash
t theme help [key]
```

> [!NOTE]
> On first run, if not configured yet, the CLI prompts you to enter the Grafana
> base URL and/or token. The token is stored in the OS keyring for secure access.
> The base URL is stored in a config file.
