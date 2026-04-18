# seasoned-wallpaper

A cross-platform wallpaper rotator with date-based overrides. Picks a random
wallpaper from a folder on each run — unless the current date matches a
user-defined override ("season"), in which case the override wins.

The CLI binary is `seasoned`.

## Install

```sh
go install github.com/floholz/seasoned-wallpaper/cmd/seasoned@latest
```

Or from a checkout:

```sh
go build -o seasoned ./cmd/seasoned
```

## Quick start

1. Copy `config.example.yaml` to the default config location:
   - Linux / macOS: `~/.config/seasoned/config.yaml`
     (or `$XDG_CONFIG_HOME/seasoned/config.yaml` if set)
   - Windows: `%APPDATA%\seasoned\config.yaml`
2. Point `wallpaper_dir` at a folder of wallpapers.
3. Run `seasoned run`.

## Commands

```
seasoned run                  # pick & apply for today, exit
seasoned next                 # force a re-roll (ignore already-set-today)
seasoned preview YYYY-MM-DD   # print what would be picked on that date, no-op
seasoned detect               # print detected platform backend and exit
seasoned seasons              # list configured seasons and their next match
seasoned daemon               # run as a long-lived background process
seasoned daemon --status      # report whether a daemon is running
seasoned reload               # tell a running daemon to reload its config
seasoned kick                 # tell a running daemon to force a re-roll
seasoned version              # print version and exit
```

Global flags:

- `--config PATH` — override config location
- `--dry-run` — resolve and print, do not apply
- `--verbose` — log decisions to stderr

Exit codes: `0` ok, `1` generic, `2` config, `3` no wallpapers, `4` backend,
`5` daemon already running, `6` no daemon running.

## How selection works

- If today falls within one of the configured `seasons`, that season wins.
- Otherwise, a random file from `wallpaper_dir` is picked.
- The last `min(5, floor(poolSize / 2))` picks per source folder are
  remembered (in the state file) and excluded from the next draw. If that
  would leave zero candidates the exclusion set resets.
- `seasoned run` is idempotent per calendar day — re-running after a
  successful apply is a no-op. `seasoned next` bypasses that check.

Priority among matching seasons:

1. Specific date (`YYYY-MM-DD`)
2. Annual date (`MM-DD`)
3. Specific date range (`YYYY-MM-DD..YYYY-MM-DD`)
4. Annual date range (`MM-DD..MM-DD`)

Overlaps **within the same tier** are a config error, reported at load time.

## Platform backends

`seasoned` does not ship a long-running daemon — it relies on whatever
native wallpaper mechanism your desktop provides.

### Linux

Auto-detection order:

1. `linux.command` template in config (if set)
2. Hyprland / wlroots + `swww` (`swww img <path>`)
3. Hyprland + `hyprctl hyprpaper`
4. Sway + `swaybg` (non-persistent; documented limitation)
5. GNOME (`gsettings set org.gnome.desktop.background picture-uri`)
6. KDE Plasma (`plasma-apply-wallpaperimage`)
7. XFCE (`xfconf-query` on every `.../last-image` property)
8. MATE (`gsettings set org.mate.background picture-filename`)
9. Cinnamon (`gsettings set org.cinnamon.desktop.background picture-uri`)
10. Fallback: `feh --bg-fill`

If nothing matches, exit 4 and hint at the `linux.command` override.

Use `seasoned detect` to see which backend was chosen without applying
anything.

### Windows

`SystemParametersInfoW(SPI_SETDESKWALLPAPER)`. No shell-out. Persists
across reboots.

### macOS

Shells out to `osascript`:

```
osascript -e 'tell application "System Events" to set picture of every desktop to POSIX file "<path>"'
```

First-run-after-login may silently no-op until the desktop receives focus —
documented quirk, accepted for v1.

## Daemon mode (v2)

Instead of wiring a timer, you can run `seasoned` as a long-lived
per-user process. The daemon re-evaluates at each local midnight, at a
configurable safety-net interval (default 6h), and on wake-from-suspend.
It calls the same `ResolveForDate` as the one-shot CLI, so there is no
behavioral divergence.

Control surface:

- **POSIX**: `SIGHUP` → reload config, `SIGUSR1` → force re-roll,
  `SIGUSR2` → treat as wake, `SIGTERM`/`SIGINT` → graceful shutdown.
- **Windows**: a sentinel directory at
  `%LOCALAPPDATA%\seasoned\control\`. Drop a file named `reload` or
  `kick` and the daemon picks it up. The `seasoned reload` and
  `seasoned kick` subcommands do this for you on any platform.
- **Config hot-reload**: the daemon watches its config file; a valid
  write triggers a reload and immediate re-evaluation. Invalid configs
  are logged and the previous config is kept.

A `daemon:` block in the config tunes these behaviors — see
`config.example.yaml`.

### Install

Shipped unit files live under `dist/`:

- **Linux (systemd user)**: `dist/systemd/seasoned.service`. Install with
  ```sh
  install -Dm0644 dist/systemd/seasoned.service \
    ~/.config/systemd/user/seasoned.service
  systemctl --user daemon-reload
  systemctl --user enable --now seasoned.service
  ```
- **macOS (launchd agent)**: `dist/launchd/dev.floholz.seasoned.plist`.
  Copy to `~/Library/LaunchAgents/` and `launchctl bootstrap gui/$UID`
  it.
- **Windows**: `dist/windows/install-startup.ps1`. Run from a shell with
  `seasoned.exe` in the current directory (or pass `-SourcePath`). Use
  `-Mode Task` for a scheduled task instead of a Startup-folder
  shortcut.

Running the daemon and a v1 timer at the same time is harmless — the
idempotency check in `ResolveForDate` makes the timer a no-op on days
the daemon already applied something.

## Scheduling (v1 one-shot)

If you prefer not to run the daemon, `seasoned` remains a one-shot
command you can wire into the OS's native scheduler:

### systemd user timer (Linux)

`~/.config/systemd/user/seasoned.service`:

```ini
[Unit]
Description=Apply the daily wallpaper

[Service]
Type=oneshot
ExecStart=%h/.local/bin/seasoned run
```

`~/.config/systemd/user/seasoned.timer`:

```ini
[Unit]
Description=Daily wallpaper rotation

[Timer]
OnCalendar=*-*-* 00:05:00
Persistent=true

[Install]
WantedBy=timers.target
```

```sh
systemctl --user daemon-reload
systemctl --user enable --now seasoned.timer
```

`Persistent=true` catches up after the machine was asleep or off.

### Windows Task Scheduler

Create a daily trigger for `seasoned.exe run`. Keep "Run whether user is
logged on or not" **off** (the Windows wallpaper API needs an interactive
desktop). "Wake the computer" should stay **off** too.

### launchd (macOS)

`~/Library/LaunchAgents/com.seasoned.daily.plist`:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.seasoned.daily</string>
  <key>ProgramArguments</key>
  <array>
    <string>/usr/local/bin/seasoned</string>
    <string>run</string>
  </array>
  <key>StartCalendarInterval</key>
  <dict>
    <key>Hour</key><integer>0</integer>
    <key>Minute</key><integer>5</integer>
  </dict>
</dict>
</plist>
```

```sh
launchctl load ~/Library/LaunchAgents/com.seasoned.daily.plist
```

## Configuration

See [`config.example.yaml`](./config.example.yaml) for the full schema with
comments. The short version:

```yaml
wallpaper_dir: ~/Pictures/wallpapers
recursive: false
extensions: [jpg, jpeg, png, webp]

seasons:
  - name: winter-holiday
    date: "12-25"
    path: ~/Pictures/wallpapers/seasons/dec-25
  - date_range: "12-01..12-24"
    path: ~/Pictures/wallpapers/seasons/december
```

## State file

Written atomically as JSON:

- Linux / macOS: `$XDG_STATE_HOME/seasoned/state.json`
  (fallback: `~/.local/state/seasoned/state.json`)
- Windows: `%LOCALAPPDATA%\seasoned\state.json`

Corruption is non-fatal — the file is reset and a warning is logged.

## Non-goals

- No bundled holiday calendar — all dates are user-defined.
- No IPC server, socket, or HTTP endpoint on the daemon. Control is via
  signals (POSIX) or the sentinel directory (Windows).
- No process supervision. systemd / launchd / Task Scheduler own restarts.
- No GUI, no tray icon, no online sources.
- Same wallpaper on all monitors.
