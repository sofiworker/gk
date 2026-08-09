package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/sofiworker/gk/ghttp/testdata/k6/internal/resources"
)

func main() {
	os.Exit(run(os.Args[1:]))
}
func run(args []string) int {
	fs := flag.NewFlagSet("resources", flag.ContinueOnError)
	output := fs.String("output", "", "output file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	b, err := json.MarshalIndent(resources.Sample(), "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	if *output != "" {
		if err := os.WriteFile(*output, b, 0644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return 2
		}
		return 0
	}
	fmt.Println(string(b))
	return 0
}
