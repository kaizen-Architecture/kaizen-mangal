package version

import (
	"fmt"
	"github.com/kaizen-architecture/kaizen-mangal/color"
	"github.com/kaizen-architecture/kaizen-mangal/constant"
	"github.com/kaizen-architecture/kaizen-mangal/icon"
	"github.com/kaizen-architecture/kaizen-mangal/style"
	"github.com/kaizen-architecture/kaizen-mangal/util"
)

func Notify() {
	erase := util.PrintErasable(fmt.Sprintf("%s Checking if new version is available...", icon.Get(icon.Progress)))
	version, err := Latest()
	erase()
	if err == nil {
		comp, err := Compare(version, constant.Version)
		if err == nil && comp <= 0 {
			return
		}
	}

	fmt.Printf(`
%s New version is available %s %s
%s

`,
		style.Fg(color.Green)("▇▇▇"),
		style.Bold(version),
		style.Faint(fmt.Sprintf("(You're on %s)", constant.Version)),
		style.Faint("https://github.com/metafates/mangal/releases/tag/v"+version),
	)

}
