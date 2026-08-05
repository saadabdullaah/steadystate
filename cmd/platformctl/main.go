package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"runtime"

	"github.com/saadabdullaah/steadystate/internal/platformctl"
)

var (
	version   = "development"
	commit    = "unknown"
	buildDate = "unknown"
	dirty     = "unknown"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	command := platformctl.NewRootCommand(platformctl.Options{Build: platformctl.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    buildDate,
		Dirty:   dirty,
		Go:      runtime.Version(),
	}})
	if err := command.ExecuteContext(ctx); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, platformctl.ErrorMessage(err))
		os.Exit(platformctl.ExitCode(err))
	}
}
