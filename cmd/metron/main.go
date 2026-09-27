package main

import (
	"flag"
	"fmt"
	"os"

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
	flags := flag.NewFlagSet("metron", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	showVersion := flags.Bool("version", false, "print version")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Metron - live Claude and Codex account limits\n\nUsage: metron [--version]\n\nKeys: r refresh, q quit, arrows scroll\n\nUses your existing Claude Code and Codex sign-ins.\nRefreshes every 60 seconds.")
		flags.PrintDefaults()
	}
	if err := flags.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "metron: unexpected arguments; use --help")
		return 2
	}
	if *showVersion {
		fmt.Printf("metron version %s\n", version)
		return 0
	}
	if !term.IsTerminal(os.Stdout.Fd()) || !term.IsTerminal(os.Stdin.Fd()) {
		fmt.Fprintln(os.Stderr, "metron: open an interactive terminal to watch account limits")
		return 1
	}
	codex := provider.NewCodex(version)
	defer codex.Close()
	model := dashboard.New([]provider.Provider{provider.NewClaude(), codex})
	defer model.Close()
	if _, err := tea.NewProgram(model).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "metron: terminal dashboard stopped unexpectedly")
		return 1
	}
	return 0
}
