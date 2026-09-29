package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/vndroid/vitrine/internal/validate"
	"github.com/vndroid/vitrine/web"
)

// validateCmd implements "vitrine validate" and returns the exit code.
func validateCmd(args []string) int {
	fset := flag.NewFlagSet("vitrine validate", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: vitrine validate [-config dir] [-strict]\n\nChecks the config folder: syntax, unknown options, types, values and\nreferences. Exits with 1 on errors (with -strict also on warnings).\n\n")
		fset.PrintDefaults()
	}
	confDir := fset.String("config", env("CONFIG", ""), "config folder to check")
	strict := fset.Bool("strict", false, "fail on warnings too")
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *confDir == "" {
		fmt.Println("no config folder given (-config or VITRINE_CONFIG), the built-in defaults are valid")
		return 0
	}
	if fi, err := os.Stat(*confDir); err != nil || !fi.IsDir() {
		fmt.Fprintf(os.Stderr, "%s: not a folder\n", *confDir)
		return 1
	}

	issues := validate.Config(validateOptions(*confDir))
	for _, iss := range issues {
		fmt.Println(iss)
	}
	errs := validate.Errors(issues)
	warns := len(issues) - errs
	switch {
	case len(issues) == 0:
		fmt.Printf("%s: configuration is valid\n", *confDir)
	default:
		fmt.Printf("%d error(s), %d warning(s)\n", errs, warns)
	}
	if errs > 0 || *strict && warns > 0 {
		return 1
	}
	return 0
}

func validateOptions(dir string) validate.Options {
	var themes []string
	if entries, err := fs.ReadDir(web.Public(), "images/themes"); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				themes = append(themes, e.Name())
			}
		}
	}
	return validate.Options{Dir: dir, Defaults: web.Conf(), Themes: themes}
}

// logIssues logs the problems of the config folder while running.
func logIssues(log *slog.Logger, dir string) {
	for _, iss := range validate.Config(validateOptions(dir)) {
		level := slog.LevelWarn
		if iss.Level == validate.Error {
			level = slog.LevelError
		}
		log.Log(context.Background(), level, "config: "+iss.String())
	}
}
