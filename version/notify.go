package version

import (
	"fmt"
	"os"

	"github.com/kaizen-architecture/kaizen-mangal/color"
	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/icon"
	"github.com/kaizen-architecture/kaizen-mangal/style"
	"github.com/kaizen-architecture/kaizen-mangal/util"
	"golang.org/x/term"
)

func Notify() {
	if os.Getenv(EnvDisableUpdateCheck) != "" {
		return
	}
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}

	erase := util.PrintErasable(fmt.Sprintf("%s Checking if new version is available...", icon.Get(icon.Progress)))
	version, err := Latest()
	erase()
	if err != nil {
		return
	}

	comp, err := Compare(version, constant.Version)
	if err != nil || comp <= 0 {
		return
	}

	fmt.Printf(`
%s New version is available %s %s
%s

`,
		style.Fg(color.Green)("▇▇▇"),
		style.Bold(version),
		style.Faint(fmt.Sprintf("(You're on %s)", constant.Version)),
		style.Faint(releasesPagePrefix+version),
	)
}
