//go:build linux

package setter

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
)

func newPlatform(opts Options) (Setter, error) {
	r := opts.Runner

	if opts.LinuxCommand != "" {
		return newTemplateSetter(opts.LinuxCommand, r)
	}

	desktop := strings.ToLower(r.Getenv("XDG_CURRENT_DESKTOP"))
	session := strings.ToLower(r.Getenv("XDG_SESSION_TYPE"))
	var tried []string

	try := func(bin string, build func() Setter) Setter {
		if _, ok := r.LookPath(bin); ok {
			return build()
		}
		tried = append(tried, bin)
		return nil
	}

	// Hyprland / wlroots compositors → swww
	if contains(desktop, "hyprland") || (session == "wayland" && contains(desktop, "wlroots")) {
		if s := try("swww", func() Setter { return newSwwwSetter(r) }); s != nil {
			return s, nil
		}
	}
	// Hyprland → hyprpaper via hyprctl
	if contains(desktop, "hyprland") {
		if s := try("hyprctl", func() Setter { return newHyprpaperSetter(r) }); s != nil {
			return s, nil
		}
	}
	// Sway → swaybg (non-persistent; best effort, user should pair with a scheduler)
	if contains(desktop, "sway") {
		if s := try("swaybg", func() Setter { return newSwaybgSetter(r) }); s != nil {
			return s, nil
		}
	}
	// GNOME
	if contains(desktop, "gnome") || contains(desktop, "unity") {
		if s := try("gsettings", func() Setter { return newGnomeSetter(r) }); s != nil {
			return s, nil
		}
	}
	// KDE Plasma
	if contains(desktop, "kde") || contains(desktop, "plasma") {
		if s := try("plasma-apply-wallpaperimage", func() Setter { return newPlasmaSetter(r) }); s != nil {
			return s, nil
		}
	}
	// XFCE
	if contains(desktop, "xfce") {
		if s := try("xfconf-query", func() Setter { return newXfconfSetter(r) }); s != nil {
			return s, nil
		}
	}
	// MATE
	if contains(desktop, "mate") {
		if s := try("gsettings", func() Setter { return newMateSetter(r) }); s != nil {
			return s, nil
		}
	}
	// Cinnamon
	if contains(desktop, "cinnamon") {
		if s := try("gsettings", func() Setter { return newCinnamonSetter(r) }); s != nil {
			return s, nil
		}
	}
	// Fallback: feh
	if s := try("feh", func() Setter { return newFehSetter(r) }); s != nil {
		return s, nil
	}

	return nil, fmt.Errorf("setter: no Linux backend detected (XDG_CURRENT_DESKTOP=%q XDG_SESSION_TYPE=%q); tried %v; set linux.command in config to override",
		r.Getenv("XDG_CURRENT_DESKTOP"), r.Getenv("XDG_SESSION_TYPE"), tried)
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }

func newTemplateSetter(tmplStr string, r CommandRunner) (Setter, error) {
	tmpl, err := template.New("linux-command").Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("setter: invalid linux.command template: %w", err)
	}
	return &cmdSetter{
		name: "linux/custom",
		apply: func(ctx context.Context, path string) error {
			var buf bytes.Buffer
			data := struct{ Path string }{Path: shellQuote(path)}
			if err := tmpl.Execute(&buf, data); err != nil {
				return fmt.Errorf("setter: render linux.command: %w", err)
			}
			return r.Run(ctx, "sh", "-c", buf.String())
		},
	}, nil
}

func newSwwwSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/swww",
		apply: func(ctx context.Context, path string) error {
			return r.Run(ctx, "swww", "img", path)
		},
	}
}

func newHyprpaperSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/hyprpaper",
		apply: func(ctx context.Context, path string) error {
			if err := r.Run(ctx, "hyprctl", "hyprpaper", "preload", path); err != nil {
				return err
			}
			return r.Run(ctx, "hyprctl", "hyprpaper", "wallpaper", ","+path)
		},
	}
}

func newSwaybgSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/swaybg",
		apply: func(ctx context.Context, path string) error {
			// swaybg is a foreground process; sway users typically want a
			// persistent backend. This invocation will block the CLI for the
			// lifetime of the call — document via README.
			return r.Run(ctx, "swaybg", "-i", path, "-m", "fill")
		},
	}
}

func newGnomeSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/gnome",
		apply: func(ctx context.Context, path string) error {
			uri := "file://" + path
			if err := r.Run(ctx, "gsettings", "set", "org.gnome.desktop.background", "picture-uri", uri); err != nil {
				return err
			}
			// picture-uri-dark exists on GNOME 42+; treat failure as non-fatal.
			_ = r.Run(ctx, "gsettings", "set", "org.gnome.desktop.background", "picture-uri-dark", uri)
			return nil
		},
	}
}

func newMateSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/mate",
		apply: func(ctx context.Context, path string) error {
			return r.Run(ctx, "gsettings", "set", "org.mate.background", "picture-filename", path)
		},
	}
}

func newCinnamonSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/cinnamon",
		apply: func(ctx context.Context, path string) error {
			return r.Run(ctx, "gsettings", "set", "org.cinnamon.desktop.background", "picture-uri", "file://"+path)
		},
	}
}

func newPlasmaSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/plasma",
		apply: func(ctx context.Context, path string) error {
			return r.Run(ctx, "plasma-apply-wallpaperimage", path)
		},
	}
}

func newXfconfSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/xfce",
		apply: func(ctx context.Context, path string) error {
			out, err := r.Output(ctx, "xfconf-query", "-c", "xfce4-desktop", "-l")
			if err != nil {
				return fmt.Errorf("xfconf-query list: %w", err)
			}
			props := filterXfceProps(string(out))
			if len(props) == 0 {
				return fmt.Errorf("xfconf: no last-image properties found under xfce4-desktop")
			}
			var firstErr error
			for _, p := range props {
				if err := r.Run(ctx, "xfconf-query", "-c", "xfce4-desktop", "-p", p, "-s", path); err != nil && firstErr == nil {
					firstErr = err
				}
			}
			return firstErr
		},
	}
}

func filterXfceProps(listOutput string) []string {
	var props []string
	for _, line := range strings.Split(listOutput, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/backdrop/") && strings.HasSuffix(line, "/last-image") {
			props = append(props, line)
		}
	}
	return props
}

func newFehSetter(r CommandRunner) Setter {
	return &cmdSetter{
		name: "linux/feh",
		apply: func(ctx context.Context, path string) error {
			return r.Run(ctx, "feh", "--bg-fill", path)
		},
	}
}

// shellQuote wraps s in single quotes, escaping any embedded single quotes.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
