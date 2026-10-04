package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/term"
	"github.com/cyakimov/metron/internal/dashboard"
	"github.com/cyakimov/metron/internal/provider"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if opts.showVersion {
		fmt.Printf("metron version %s\n", version)
		return 0
	}
	if !term.IsTerminal(os.Stdout.Fd()) || !term.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(os.Stderr, "metron: open an interactive terminal to watch account limits")
		return 1
	}
	codex := provider.NewCodex(version)
	defer codex.Close()
	model := dashboard.New([]provider.Provider{provider.NewClaude(), codex}, opts.refreshIntervals, !opts.noMotion)
	defer model.Close()
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "metron: terminal dashboard stopped unexpectedly")
		return 1
	}
	return 0
}

type options struct {
	showVersion      bool
	noMotion         bool
	refreshIntervals map[string]time.Duration
}

func parseOptions(args []string, output io.Writer) (options, error) {
	flags := flag.NewFlagSet("metron", flag.ContinueOnError)
	flags.SetOutput(output)
	showVersion := flags.Bool("version", false, "print version")
	noMotion := flags.Bool("no-motion", false, "disable animations; press a to toggle motion")
	refreshInterval := flags.Duration("refresh-interval", 0, "refresh `duration` for both platforms; platform flags take precedence")
	claudeInterval := flags.Duration("claude-refresh-interval", 5*time.Minute, "Claude refresh `duration`; overrides --refresh-interval")
	codexInterval := flags.Duration("codex-refresh-interval", time.Minute, "Codex refresh `duration`; overrides --refresh-interval")
	flags.Usage = func() {
		fmt.Fprintln(output, "Metron - live Claude and Codex account limits\n\nUsage: metron [options]\n\nKeys: r refresh, a toggle motion, q quit, arrows scroll\n\nUses your existing Claude Code and Codex sign-ins.\nDefaults: Claude every 5 minutes, Codex every minute.\nIntervals must be positive durations, such as 30s, 5m, or 1h.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 {
		err := fmt.Errorf("metron: unexpected arguments; use --help")
		fmt.Fprintln(output, err)
		return options{}, err
	}
	values := map[string]time.Duration{
		"refresh-interval":        *refreshInterval,
		"claude-refresh-interval": *claudeInterval,
		"codex-refresh-interval":  *codexInterval,
	}
	supplied := make(map[string]bool)
	var validationErr error
	flags.Visit(func(f *flag.Flag) {
		supplied[f.Name] = true
		if value, ok := values[f.Name]; ok && value <= 0 && validationErr == nil {
			validationErr = fmt.Errorf("metron: --%s must be a positive duration", f.Name)
		}
	})
	if validationErr != nil {
		fmt.Fprintln(output, validationErr)
		return options{}, validationErr
	}
	intervals := map[string]time.Duration{"claude": *claudeInterval, "codex": *codexInterval}
	if supplied["refresh-interval"] {
		for id := range intervals {
			intervals[id] = *refreshInterval
		}
	}
	if supplied["claude-refresh-interval"] {
		intervals["claude"] = *claudeInterval
	}
	if supplied["codex-refresh-interval"] {
		intervals["codex"] = *codexInterval
	}
	return options{showVersion: *showVersion, noMotion: *noMotion, refreshIntervals: intervals}, nil
}
