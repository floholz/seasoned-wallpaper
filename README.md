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
seasoned version              # print version and exit
```

Global flags:

- `--config PATH` — override config location
- `--dry-run` — resolve and print, do not apply
- `--verbose` — log decisions to stderr

Exit codes: `0` ok, `1` generic, `2` config, `3` no wallpapers, `4` backend.

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

## Scheduling

`seasoned` is a one-shot command. Wire it to the OS's native scheduler:

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

## Non-goals (for v1)

- No bundled holiday calendar — all dates are user-defined.
- No long-running daemon; scheduling is the OS's job.
- No GUI, no tray icon, no online sources.
- Same wallpaper on all monitors.
