package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"crdx.org/col"
	"crdx.org/io/internal/sim"
)

const usage = `simulate — the Responses API

Usage:
    simulate --scenario <path> [--address <address>]
    simulate --help

Options:
    -s, --scenario <path>       The scenario
    -a, --address <address>     Where to listen [default: localhost:8080]
    -h, --help                  Show this help
`

const readTimeout = 30 * time.Second

func main() {
	col.Init()

	if err := run(); err != nil {
		if errors.Is(err, errHelp) {
			fmt.Print(usage)
			return
		}

		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var errHelp = errors.New("help")

type options struct {
	scenario string // the scenario file
	address  string // where the endpoint listens
}

func parse(arguments []string) (options, error) {
	self := options{address: "localhost:8080"}

	for index := 0; index < len(arguments); index++ {
		argument := arguments[index]

		wants := func() bool { return index+1 < len(arguments) }

		switch argument {
		case "-s", "--scenario":
			if !wants() {
				return self, fmt.Errorf("%s wants a path", argument)
			}

			index++
			self.scenario = arguments[index]

		case "-a", "--address":
			if !wants() {
				return self, fmt.Errorf("%s wants an address", argument)
			}

			index++
			self.address = arguments[index]

		case "-h", "--help":
			return self, errHelp

		default:
			return self, fmt.Errorf("no such option: %s", argument)
		}
	}

	if self.scenario == "" {
		return self, errors.New("a scenario is needed")
	}

	return self, nil
}

func run() error {
	options, err := parse(os.Args[1:])
	if err != nil {
		return err
	}

	scenario, err := sim.Read(options.scenario)
	if err != nil {
		return err
	}

	endpoint := sim.New(scenario)

	server := &http.Server{
		Addr:              options.address,
		Handler:           endpoint,
		ReadHeaderTimeout: readTimeout,
	}

	fmt.Printf("%s %s\n", col.Green("listening on"), col.White("http://"+options.address))
	fmt.Printf(
		"%s %s, %d turns%s\n",
		col.Green("answering as"),
		col.White(scenario.Model),
		len(scenario.Turns),
		loops(scenario.Loop),
	)

	return server.ListenAndServe()
}

func loops(looping bool) string {
	if looping {
		return ", looping"
	}

	return ""
}
