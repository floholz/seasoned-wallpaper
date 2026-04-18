// Command seasoned applies a wallpaper for today (or a given date) based on
// a YAML config. See SPEC.md for the contract.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/floholz/seasoned-wallpaper/internal/config"
	"github.com/floholz/seasoned-wallpaper/internal/resolver"
	"github.com/floholz/seasoned-wallpaper/internal/season"
	"github.com/floholz/seasoned-wallpaper/internal/setter"
	"github.com/floholz/seasoned-wallpaper/internal/state"
)

// version is stamped at build time via -ldflags "-X main.version=...".
var version = "dev"

// Exit codes (SPEC.md).
const (
	exitOK           = 0
	exitGeneric      = 1
	exitConfigError  = 2
	exitNoWallpapers = 3
	exitBackendError = 4
)

const dateFmt = "2006-01-02"

type globalFlags struct {
	configPath string
	dryRun     bool
	verbose    bool
}

func main() {
	os.Exit(run(os.Args))
}

func run(argv []string) int {
	fs := flag.NewFlagSet("seasoned", flag.ContinueOnError)
	g := &globalFlags{}
	fs.StringVar(&g.configPath, "config", "", "path to config file (overrides default search order)")
	fs.BoolVar(&g.dryRun, "dry-run", false, "resolve and print, do not apply")
	fs.BoolVar(&g.verbose, "verbose", false, "log decision process to stderr")
	fs.SetOutput(os.Stderr)
	fs.Usage = func() { printUsage(os.Stderr) }

	if err := fs.Parse(argv[1:]); err != nil {
		return exitGeneric
	}
	args := fs.Args()
	if len(args) == 0 {
		printUsage(os.Stderr)
		return exitGeneric
	}

	setupLogger(g.verbose)

	switch args[0] {
	case "run":
		return cmdRun(g, args[1:], false)
	case "next":
		return cmdRun(g, args[1:], true)
	case "preview":
		return cmdPreview(g, args[1:])
	case "detect":
		return cmdDetect(g)
	case "seasons":
		return cmdSeasons(g)
	case "version":
		fmt.Println(version)
		return exitOK
	case "help", "-h", "--help":
		printUsage(os.Stdout)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "seasoned: unknown subcommand %q\n", args[0])
		printUsage(os.Stderr)
		return exitGeneric
	}
}

func setupLogger(verbose bool) {
	level := slog.LevelWarn
	if verbose {
		level = slog.LevelDebug
	}
	h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(h))
}

func printUsage(w io.Writer) {
	fmt.Fprint(w, `Usage: seasoned [flags] <command> [args]

Commands:
  run                   pick & apply a wallpaper for today, exit
  next                  force a re-roll (ignore "already set today")
  preview YYYY-MM-DD    print what would be picked on that date (no-op)
  detect                print detected platform backend and exit
  seasons               list configured seasons and their next match
  version               print version and exit

Global flags:
  --config PATH         override default config location
  --dry-run             resolve and print the chosen wallpaper, don't apply
  --verbose             log decision process to stderr

Exit codes: 0 ok, 1 generic, 2 config, 3 no wallpapers, 4 backend.
`)
}

// loadConfig loads the config. If required is false and the default path
// doesn't exist, returns (nil, 0) so the caller can proceed without it.
func loadConfig(g *globalFlags, required bool) (*config.Config, int) {
	cfg, err := config.Load(g.configPath)
	if err == nil {
		return cfg, 0
	}
	if !required && g.configPath == "" && errors.Is(err, fs.ErrNotExist) {
		return nil, 0
	}
	fmt.Fprintln(os.Stderr, err)
	return nil, exitConfigError
}

func newRNG() *rand.Rand {
	return rand.New(rand.NewPCG(uint64(time.Now().UnixNano()), uint64(os.Getpid())))
}

// cmdRun implements `run` and `next`. If force is false, an already-applied
// state for today is a no-op.
func cmdRun(g *globalFlags, args []string, force bool) int {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, "seasoned: unexpected arguments")
		return exitGeneric
	}
	cfg, rc := loadConfig(g, true)
	if rc != 0 {
		return rc
	}

	statePath, err := state.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	st, err := state.Load(statePath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}

	now := time.Now()
	today := now.Format(dateFmt)

	if !force && st.LastAppliedDate == today && st.LastAppliedPath != "" {
		slog.Debug("already applied today, skipping", "date", today, "path", st.LastAppliedPath)
		return exitOK
	}

	res, err := resolver.Resolve(now, cfg, st, newRNG())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if strings.Contains(err.Error(), "no matching files") || strings.Contains(err.Error(), "no source") {
			return exitNoWallpapers
		}
		return exitGeneric
	}

	slog.Debug("resolved", "path", res.Path, "season", res.SeasonName, "from_pool", res.FromPool, "pool_size", res.PoolSize)
	fmt.Println(res.Path)

	if g.dryRun {
		return exitOK
	}

	set, err := setter.New(setter.Options{LinuxCommand: cfg.LinuxCommand})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitBackendError
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := set.Apply(ctx, res.Path); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitBackendError
	}

	next := resolver.RecordResolution(st, now, res)
	if err := state.Save(statePath, next); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	return exitOK
}

func cmdPreview(g *globalFlags, args []string) int {
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "usage: seasoned preview YYYY-MM-DD")
		return exitGeneric
	}
	when, err := time.ParseInLocation(dateFmt, args[0], time.Local)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid date %q: %v\n", args[0], err)
		return exitGeneric
	}

	cfg, rc := loadConfig(g, true)
	if rc != 0 {
		return rc
	}

	statePath, err := state.DefaultPath()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitGeneric
	}
	st, _ := state.Load(statePath)
	if st == nil {
		st = &state.State{}
	}

	res, err := resolver.Resolve(when, cfg, st, newRNG())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if strings.Contains(err.Error(), "no matching files") {
			return exitNoWallpapers
		}
		return exitGeneric
	}

	origin := "pool"
	if !res.FromPool {
		origin = "season=" + displaySeasonName(res.SeasonName)
	}
	fmt.Printf("%s  %s  (%s, pool=%d)\n", when.Format(dateFmt), res.Path, origin, res.PoolSize)
	return exitOK
}

func cmdDetect(g *globalFlags) int {
	cfg, rc := loadConfig(g, false)
	if rc != 0 {
		return rc
	}
	var opts setter.Options
	if cfg != nil {
		opts.LinuxCommand = cfg.LinuxCommand
	}
	s, err := setter.New(opts)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitBackendError
	}
	fmt.Println(s.Describe())
	return exitOK
}

func cmdSeasons(g *globalFlags) int {
	cfg, rc := loadConfig(g, true)
	if rc != 0 {
		return rc
	}
	if len(cfg.Seasons) == 0 {
		fmt.Println("no seasons configured")
		return exitOK
	}
	now := time.Now()
	tw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tKIND\tDATE\tNEXT MATCH\tPATH")
	for i := range cfg.Seasons {
		s := &cfg.Seasons[i]
		nextStr := "—"
		if next, ok := season.NextMatch(s, now); ok {
			nextStr = next.Format(dateFmt)
			if next.Equal(truncDay(now)) {
				nextStr += " (today)"
			}
		} else {
			nextStr = "past"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			displaySeasonName(s.Name), s.Kind.String(), seasonDateStr(s), nextStr, s.Path)
	}
	tw.Flush()
	return exitOK
}

func truncDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

func displaySeasonName(n string) string {
	if n == "" {
		return "<unnamed>"
	}
	return n
}

func seasonDateStr(s *season.Spec) string {
	switch s.Kind {
	case season.KindSpecificDate:
		return fmt.Sprintf("%04d-%02d-%02d", s.StartYear, s.StartMonth, s.StartDay)
	case season.KindAnnualDate:
		return fmt.Sprintf("%02d-%02d", s.StartMonth, s.StartDay)
	case season.KindSpecificRange:
		return fmt.Sprintf("%04d-%02d-%02d..%04d-%02d-%02d",
			s.StartYear, s.StartMonth, s.StartDay, s.EndYear, s.EndMonth, s.EndDay)
	case season.KindAnnualRange:
		return fmt.Sprintf("%02d-%02d..%02d-%02d",
			s.StartMonth, s.StartDay, s.EndMonth, s.EndDay)
	}
	return "?"
}
