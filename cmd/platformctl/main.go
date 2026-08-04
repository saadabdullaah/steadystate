package main

import (
	"context"
	"fmt"
	"os"
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
	command := platformctl.NewRootCommand(platformctl.Options{Build: platformctl.BuildInfo{
		Version: version,
		Commit:  commit,
		Date:    buildDate,
		Dirty:   dirty,
		Go:      runtime.Version(),
	}})
	if err := command.ExecuteContext(context.Background()); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, platformctl.ErrorMessage(err))
		os.Exit(platformctl.ExitCode(err))
	}
}
