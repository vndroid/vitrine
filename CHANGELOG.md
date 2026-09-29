# Changelog

Versions follow `major.minor.patch`: small features and fixes raise the
patch version, larger feature changes the minor version. The version lives
in `cmd/vitrine/version.go`; releases are tagged by hand.

## 0.2.3 - 2026-09-29

- shared files and folder index files are opened once, after the access
  checks, and have to be the very file that was checked; inside the root
  the open can't leave the root, so replacing a file or folder with a
  symbolic link between the check and the open (TOCTOU) serves nothing
  else

## 0.2.2 - 2026-09-29

- every response carries `X-Frame-Options: SAMEORIGIN` and
  `Content-Security-Policy: frame-ancestors 'self'`, so other sites can't
  frame vitrine (clickjacking); previews framed by vitrine itself keep
  working

## 0.2.1 - 2026-09-29

- `vitrine -v` / `--version` prints the version with branch, revision,
  build date, platform, Go runtime and build tags; labels are gray on
  a terminal (`NO_COLOR` turns colors off)
- the Docker image records the build date and takes the commit as
  `REVISION` and `BRANCH` build arguments

## 0.2.0 - 2026-09-29

- `vitrine validate [-config dir] [-strict]` checks a config folder:
  syntax (with line and column), unknown options with suggestions,
  types, allowed values and ranges, hidden patterns, the password hash
  and references to themes, languages and file types
- the same checks are logged at start and after every config reload;
  config load errors report line and column
- file types for README, LICENSE, AUTHORS, INSTALL and Makefile, whose
  text preview styles were configured but never matched

## 0.1.10 - 2026-09-29

- info page: show the Go runtime and the platform as separate checks

## 0.1.9 - 2026-09-29

- limit thumbnail requests: 2 in progress per client and 16 in total,
  more are answered with 429 ERR_BUSY; at most 32 renders wait for a
  slot; one request stops generating after 30 seconds

## 0.1.8 - 2026-09-29

- `-access-log` logs every request with the real client address (after
  trusted proxies), status, bytes, duration and user agent; aborted
  downloads are marked
- `-log-format json` for structured logs

## 0.1.7 - 2026-09-29

- faster long folder listings in the details view: rows outside the
  viewport are skipped by the browser (content-visibility), rows are
  inserted and sorted in one go; for 5000 entries opening takes ~2.4 s
  instead of ~3.3 s, sorting ~0.6 s instead of ~1.1 s, relayouts ~75 ms
  instead of ~290 ms

## 0.1.6 - 2026-09-29

- update marked from 4.0.10 to 18.0.14 (markdown previews and custom
  header/footer files); headings no longer get automatic ids
- polyfill Array/String.prototype.at and Object.hasOwn for browsers
  without ES2022, the frontend keeps supporting ES2020 browsers
- the frontend build fails if the bundle (dependencies included) uses
  built-ins newer than ES2020 that are not polyfilled

## 0.1.5 - 2026-09-29

- thumbnails for WebP (native), AVIF and HEIC (ImageMagick) images;
  WebP and AVIF previews in the browser
- docs: Caddy reverse proxy configuration

## 0.1.4 - 2026-09-29

- image previews show large images while they load
- images the browser can't open show a message and a download link

## 0.1.3 - 2026-09-29

- syntax highlighting for code blocks in markdown previews

## 0.1.2 - 2026-09-29

- center the message for unplayable media

## 0.1.1 - 2026-09-29

- videos and audio files the browser can't play show a message and a
  download link instead of a spinner that never ends

## 0.1.0 - 2026-09-29

First release: a Go rewrite of the h5fs backend, serving the h5fs frontend.

- single binary serving the frontend, the shared files (with HTTP Range)
  and the h5fs compatible API; Docker image with ffmpeg and ImageMagick
- listing, search, custom header/footer, 35 languages, info page
- packaged tar/zip downloads built natively, with the h5fs limits
- thumbnails for images (EXIF orientation aware), videos and PDFs, with
  a size limited cache; `thumbnails.exif` uses embedded EXIF thumbnails
  that are large enough and not letterboxed
- admin login compatible with h5fs password hashes, `vitrine passwd`
- configuration compatible with h5fs, merged over the defaults and
  reloaded while running (every 2 seconds and on SIGHUP)
- `-base-path` to serve below a path of a site
- `-follow-symlinks` to serve links leaving the shared folder
- `-trusted-proxy` for reverse proxies (real client address, TLS)
- hidden entries are neither listed nor served, shared HTML/SVG files
  are sandboxed
