package main

import (
	"github.com/goyek/goyek/v3"
	goyekcmd "github.com/goyek/x/cmd"
)

var testTask = goyek.Define(goyek.Task{
	Name:  "test",
	Usage: "run tests for the application and build modules",
	Action: func(a *goyek.A) {
		runCommand(a, "go", []string{"test", "./..."})
		runCommand(a, "go", []string{"test", "./..."}, goyekcmd.Dir("build"))
	},
})

var lintTask = goyek.Define(goyek.Task{
	Name:  "lint",
	Usage: "run golangci-lint for the application and build modules",
	Action: func(a *goyek.A) {
		runCommand(a, "golangci-lint", []string{"run", "./..."})
		runCommand(
			a,
			"golangci-lint",
			[]string{"run", "./..."},
			goyekcmd.Dir("build"),
		)
	},
})

var validateTask = goyek.Define(goyek.Task{
	Name:  "validate",
	Usage: "run all tests and linters",
	Deps:  goyek.Deps{testTask, lintTask},
})
