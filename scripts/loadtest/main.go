package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "preflight", "generate", "prepare", "run", "verify", "report", "clean", "smoke", "calibrate", "inject-fault":
		fmt.Fprintf(os.Stderr, "%s is not implemented yet\n", os.Args[1])
		os.Exit(2)
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: loadtest <preflight|generate|prepare|run|verify|report|clean|smoke|calibrate|inject-fault>")
}
