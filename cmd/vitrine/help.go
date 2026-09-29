package main

import (
	"flag"
	"fmt"
	"io"
	"strings"
)

// isHelpFlag reports whether the arguments ask for the help.
func isHelpFlag(args []string) bool {
	if len(args) != 1 {
		return false
	}
	switch args[0] {
	case "-h", "--help", "-help", "help":
		return true
	}
	return false
}

// envName is the environment variable of a flag.
func envName(flagName string) string {
	return "VITRINE_" + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// printHelp writes the usage: the commands and every flag of the server
// with its environment variable and default.
func printHelp(w io.Writer, server *flag.FlagSet) {
	fmt.Fprint(w, `vitrine - a modern HTML5 directory index in a single binary

Usage:
  vitrine [flags]                      serve a folder (-root is required)
  vitrine validate [-config dir] [-strict]
                                       check a config folder
  vitrine passwd                       print a bcrypt hash for the "passhash" option
  vitrine -v, --version                print the version and build information
  vitrine -h, --help                   show this help

Flags:
`)
	server.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		head := "  -" + f.Name
		if name != "" {
			head += " " + name
		}
		fmt.Fprintf(w, "%s\n        %s", head, usage)
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Fprintf(w, " (default %q)", f.DefValue)
		}
		fmt.Fprintf(w, "\n        environment: %s\n", envName(f.Name))
	})
	fmt.Fprintf(w, `
Flags of "vitrine validate" ("vitrine validate -h" for details):
  -config dir
        config folder to check
        environment: %s
  -strict
        fail on warnings too

More: https://github.com/vndroid/vitrine
`, envName("config"))
}
