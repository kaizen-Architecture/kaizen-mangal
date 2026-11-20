package main

import (
	"github.com/kaizen-architecture/kaizen-mangal/cmd"
	"github.com/kaizen-architecture/kaizen-mangal/config"
	"github.com/kaizen-architecture/kaizen-mangal/log"
	"github.com/samber/lo"
)

func main() {
	lo.Must0(config.Setup())
	lo.Must0(log.Setup())
	cmd.Execute()
}
