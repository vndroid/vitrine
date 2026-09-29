// Command vitrine serves a folder as a browsable HTML5 directory index.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/vndroid/vitrine/internal/auth"
	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/internal/server"
	"github.com/vndroid/vitrine/internal/tree"
	"github.com/vndroid/vitrine/web"
	"golang.org/x/term"
)

// configWatchInterval is how often the config folder is checked.
const configWatchInterval = 2 * time.Second

// version is set at build time with -ldflags "-X main.version=...".
var version = ""

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "version":
			fmt.Println(buildVersion())
			return
		case "passwd":
			if err := passwd(); err != nil {
				fmt.Fprintln(os.Stderr, "vitrine:", err)
				os.Exit(1)
			}
			return
		}
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vitrine:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fset := flag.NewFlagSet("vitrine", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: vitrine [flags]\n       vitrine passwd   print a password hash for the \"passhash\" option\n       vitrine version\n\nFlags (also settable as VITRINE_<NAME> environment variables):\n")
		fset.PrintDefaults()
	}
	root := fset.String("root", env("ROOT", ""), "folder to share (required)")
	listen := fset.String("listen", env("LISTEN", ":8080"), "address to listen on")
	confDir := fset.String("config", env("CONFIG", ""), "config folder overriding the defaults (options.json, types.json, l10n/, ext/)")
	cacheDir := fset.String("cache", env("CACHE", defaultCacheDir()), "cache folder for thumbnails")
	proxies := fset.String("trusted-proxy", env("TRUSTED_PROXY", ""), "comma separated IPs/CIDRs of reverse proxies whose X-Real-IP, X-Forwarded-For and X-Forwarded-Proto headers are trusted")
	if err := fset.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if *root == "" {
		fset.Usage()
		return errors.New("-root is required")
	}

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	slog.SetDefault(log)

	cfg, err := config.Load(*confDir, web.Conf())
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	trusted, err := server.ParseTrustedProxies(*proxies)
	if err != nil {
		return err
	}
	if *cacheDir != "" {
		if err := os.MkdirAll(*cacheDir, 0o755); err != nil {
			return fmt.Errorf("cache: %w", err)
		}
	}
	tr, err := tree.New(*root, cfg, *cacheDir, *confDir)
	if err != nil {
		return err
	}

	srv := server.New(server.Options{
		Tree:           tr,
		Config:         cfg,
		Public:         web.Public(),
		ConfigDir:      *confDir,
		CacheDir:       *cacheDir,
		Version:        buildVersion(),
		TrustedProxies: trusted,
		Logger:         log,
	})
	httpSrv := &http.Server{
		Addr:              *listen,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
		// no WriteTimeout: downloads may take long, archives enforce limits
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// like h5fs, config changes apply without a restart: the config folder
	// is checked every few seconds, SIGHUP reloads immediately
	go cfg.Watch(ctx, configWatchInterval, log)
	hup := make(chan os.Signal, 1)
	signal.Notify(hup, syscall.SIGHUP)
	go func() {
		for range hup {
			if err := cfg.Reload(); err != nil {
				log.Error("config reload failed, keeping the current config", "err", err)
			} else {
				log.Info("config reloaded (SIGHUP)")
			}
		}
	}()
	errc := make(chan error, 1)
	go func() {
		log.Info("vitrine started", "version", buildVersion(), "listen", *listen, "root", tr.Root())
		errc <- httpSrv.ListenAndServe()
	}()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return httpSrv.Shutdown(shutdownCtx)
}

// passwd reads a password (without echo on a terminal) and prints its
// bcrypt hash for the "passhash" option.
func passwd() error {
	var pass string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "Password: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "Repeat: ")
		again, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		if string(b) != string(again) {
			return errors.New("passwords do not match")
		}
		pass = string(b)
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return err
		}
		pass = strings.TrimRight(line, "\r\n")
	}
	hash, err := auth.Hash(pass)
	if err != nil {
		return err
	}
	fmt.Println(hash)
	return nil
}

func env(name, def string) string {
	if v, ok := os.LookupEnv("VITRINE_" + name); ok {
		return v
	}
	return def
}

func defaultCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "vitrine")
}

func buildVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			return info.Main.Version
		}
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" && len(s.Value) >= 7 {
				return "dev-" + s.Value[:7]
			}
		}
	}
	return "dev"
}
