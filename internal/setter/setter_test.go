package setter

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
)

// fakeRunner records every invocation and lets tests control env + PATH lookups.
type fakeRunner struct {
	env      map[string]string
	binaries map[string]bool   // names available on PATH
	outputs  map[string][]byte // canned Output() responses, keyed by "<name> <args...>"
	calls    []call
	// fail specifies which calls should fail; keyed like outputs.
	fail map[string]error
}

type call struct {
	kind string // "run" or "output"
	name string
	args []string
}

func newFake() *fakeRunner {
	return &fakeRunner{
		env:      map[string]string{},
		binaries: map[string]bool{},
		outputs:  map[string][]byte{},
		fail:     map[string]error{},
	}
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) error {
	key := cmdKey(name, args)
	f.calls = append(f.calls, call{kind: "run", name: name, args: append([]string(nil), args...)})
	if err, ok := f.fail[key]; ok {
		return err
	}
	return nil
}

func (f *fakeRunner) Output(_ context.Context, name string, args ...string) ([]byte, error) {
	key := cmdKey(name, args)
	f.calls = append(f.calls, call{kind: "output", name: name, args: append([]string(nil), args...)})
	if err, ok := f.fail[key]; ok {
		return nil, err
	}
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	return nil, nil
}

func (f *fakeRunner) LookPath(name string) (string, bool) {
	if f.binaries[name] {
		return "/usr/bin/" + name, true
	}
	return "", false
}

func (f *fakeRunner) Getenv(key string) string { return f.env[key] }

func cmdKey(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func TestNew_LinuxCommandOverride(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	f := newFake()
	s, err := New(Options{LinuxCommand: "swww img {{.Path}}", Runner: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Apply(context.Background(), "/wall/paper.jpg"); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if len(f.calls) != 1 {
		t.Fatalf("calls = %d", len(f.calls))
	}
	c := f.calls[0]
	if c.name != "sh" || c.args[0] != "-c" || !strings.Contains(c.args[1], "/wall/paper.jpg") {
		t.Errorf("unexpected call %+v", c)
	}
	if !strings.Contains(c.args[1], "swww img") {
		t.Errorf("template not rendered: %s", c.args[1])
	}
}

func TestDetectLinux_Hyprland_Swww(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	f := newFake()
	f.env["XDG_CURRENT_DESKTOP"] = "Hyprland"
	f.env["XDG_SESSION_TYPE"] = "wayland"
	f.binaries["swww"] = true
	s, err := New(Options{Runner: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Describe() != "linux/swww" {
		t.Errorf("describe = %q", s.Describe())
	}
	if err := s.Apply(context.Background(), "/w.jpg"); err != nil {
		t.Fatal(err)
	}
	c := f.calls[0]
	if c.name != "swww" || c.args[0] != "img" || c.args[1] != "/w.jpg" {
		t.Errorf("bad call %+v", c)
	}
}

func TestDetectLinux_GNOME_SetsBothURIs(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	f := newFake()
	f.env["XDG_CURRENT_DESKTOP"] = "GNOME"
	f.binaries["gsettings"] = true
	s, err := New(Options{Runner: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Apply(context.Background(), "/w.jpg"); err != nil {
		t.Fatal(err)
	}
	if len(f.calls) != 2 {
		t.Fatalf("expected 2 gsettings calls, got %d", len(f.calls))
	}
	for i, key := range []string{"picture-uri", "picture-uri-dark"} {
		c := f.calls[i]
		if c.name != "gsettings" || c.args[2] != key || !strings.HasSuffix(c.args[3], "/w.jpg") {
			t.Errorf("call %d = %+v", i, c)
		}
	}
}

func TestDetectLinux_XFCE_SetsAllLastImageProps(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	f := newFake()
	f.env["XDG_CURRENT_DESKTOP"] = "XFCE"
	f.binaries["xfconf-query"] = true
	f.outputs["xfconf-query -c xfce4-desktop -l"] = []byte(`
/backdrop/screen0/monitor0/workspace0/last-image
/backdrop/screen0/monitor0/workspace0/color-style
/backdrop/screen0/monitor1/workspace0/last-image
`)
	s, err := New(Options{Runner: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := s.Apply(context.Background(), "/w.jpg"); err != nil {
		t.Fatal(err)
	}
	// 1 Output() + 2 Run() (one per last-image prop).
	runs := 0
	for _, c := range f.calls {
		if c.kind == "run" {
			runs++
		}
	}
	if runs != 2 {
		t.Errorf("expected 2 set calls, got %d (%+v)", runs, f.calls)
	}
}

func TestDetectLinux_FallbackFeh(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	f := newFake()
	f.env["XDG_CURRENT_DESKTOP"] = "i3"
	f.binaries["feh"] = true
	s, err := New(Options{Runner: f})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Describe() != "linux/feh" {
		t.Errorf("describe = %q", s.Describe())
	}
}

func TestDetectLinux_NothingAvailable(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	f := newFake()
	_, err := New(Options{Runner: f})
	if err == nil || !strings.Contains(err.Error(), "no Linux backend") {
		t.Fatalf("expected detection failure, got %v", err)
	}
}

// Apply failure should surface; GNOME's picture-uri-dark failure is tolerated
// (tested here to avoid a regression).
func TestGnome_PrimaryFailurePropagates(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	f := newFake()
	f.env["XDG_CURRENT_DESKTOP"] = "GNOME"
	f.binaries["gsettings"] = true
	f.fail[cmdKey("gsettings", []string{"set", "org.gnome.desktop.background", "picture-uri", "file:///w.jpg"})] = fmt.Errorf("boom")
	s, _ := New(Options{Runner: f})
	if err := s.Apply(context.Background(), "/w.jpg"); err == nil {
		t.Fatal("expected primary failure")
	}
}
