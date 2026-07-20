package main

import (
	"os"

	"github.com/upmio/kubespray-upm/upmctl/internal/app"
	"github.com/upmio/kubespray-upm/upmctl/internal/cli"
	"github.com/upmio/kubespray-upm/upmctl/internal/runner"
)

func main() {
	service := app.New(runner.NewExecRunner())
	command := cli.New(service, os.Stdout, os.Stderr)
	os.Exit(command.Run(os.Args[1:]))
}
