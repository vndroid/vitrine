// Command vitrine serves a folder as a browsable HTML5 directory index.
package main

import (
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
	"syscall"
	"time"

	"github.com/vndroid/vitrine/internal/config"
	"github.com/vndroid/vitrine/internal/server"
	"github.com/vndroid/vitrine/internal/tree"
	"github.com/vndroid/vitrine/web"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = ""

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(buildVersion())
		return
	}
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "vitrine:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fset := flag.NewFlagSet("vitrine", flag.ContinueOnError)
	fset.Usage = func() {
		fmt.Fprintf(fset.Output(), "Usage: vitrine [flags]\n       vitrine version\n\nFlags (also settable as VITRINE_<NAME> environment variables):\n")
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
