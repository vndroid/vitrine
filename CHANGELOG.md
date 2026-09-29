# Changelog

Versions follow `major.minor.patch`: small features and fixes raise the
patch version, larger feature changes the minor version.

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
