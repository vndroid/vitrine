# Changelog

Versions follow `major.minor.patch`: small features and fixes raise the
patch version, larger feature changes the minor version.

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
