package main

import (
	"fmt"
	"os"
)

const version = "0.1.4"

func main() {
	os.Exit(dispatch(os.Args[1:]))
}

func dispatch(args []string) int {
	if len(args) == 0 {
		usage()
		return exitUsage
	}
	cmd, rest := args[0], args[1:]
	switch cmd {
	case "packs":
		return cmdPacks(rest)
	case "describe":
		return cmdDescribe(rest)
	case "work":
		return cmdWork(rest)
	case "status":
		return cmdStatus(rest)
	case "fetch":
		return cmdFetch(rest)
	case "sql":
		return cmdSQL(rest)
	case "compute":
		return cmdCompute(rest)
	case "verify":
		return cmdVerify(rest)
	case "sideload":
		return cmdSideload(rest)
	case "submit":
		return cmdSubmit(rest)
	case "doctor":
		return cmdDoctor(rest)
	case "version", "--version", "-v":
		fmt.Printf("integer-atlas %s\n", version)
		return exitOK
	case "help", "--help", "-h":
		usage()
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n", cmd)
		usage()
		return exitUsage
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `integer-atlas — query and contribute to the Integer Atlas dataset

usage: integer-atlas <command> [options]

commands:
  packs   describe   work   status        discover what's published
  fetch   sql                             get data, then query it
  compute   verify   sideload   submit    compute & contribute shards
  doctor   version   help                 diagnostics & info

global flags: --workspace DIR  --registry URL|PATH  --release REF  --refresh
              --json  -y/--yes  --log-level L
run "integer-atlas <command> -h" for a command's options
`)
}
