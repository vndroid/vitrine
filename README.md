# vitrine

A modern HTML5 directory index for sharing files over the web, shipped as a
single Go binary.

vitrine is a rewrite of [h5fs](https://github.com/vndroid/h5fs), itself a
continuation of [h5ai](https://github.com/lrsjng/h5ai) by Lars Jung. It
replaces the PHP backend, and the web server configuration it depended on,
with a self-contained server that serves the files itself.

> **Status:** early releases. The backend is a complete rewrite in Go; the
> frontend is the h5fs frontend, maintained in [`frontend/`](frontend).

## Features

- browse folders in details, grid and icon views, with tree, breadcrumb,
  sorting, filter and search
- previews for images, audio, video (streamed with HTTP Range), text,
  markdown and source code
- thumbnails for images (EXIF orientation aware), videos (ffmpeg) and
  PDF/PostScript (ImageMagick), cached with a size limit
- packaged downloads as tar or zip, streamed with limits
- custom header and footer per folder, 35 languages, QR codes
- admin info page with login (bcrypt, rate limited)

## Run

```sh
go install github.com/vndroid/vitrine/cmd/vitrine@latest
vitrine -root /srv/share
```

Then open http://localhost:8080/.

| Flag | Environment | Default | |
|---|---|---|---|
| `-root` | `VITRINE_ROOT` | | folder to share (required) |
| `-listen` | `VITRINE_LISTEN` | `:8080` | listen address |
| `-config` | `VITRINE_CONFIG` | | config folder, see below |
| `-cache` | `VITRINE_CACHE` | user cache dir | thumbnail cache |
| `-trusted-proxy` | `VITRINE_TRUSTED_PROXY` | | reverse proxies, see below |

`vitrine passwd` prints a password hash for the admin login,
`vitrine version` the version.

### Docker

```sh
docker build -t vitrine .
docker run -d -p 8080:8080 \
  -v /srv/share:/share:ro \
  -v /srv/vitrine-config:/config:ro -e VITRINE_CONFIG=/config \
  -v vitrine-cache:/cache \
  vitrine
```

The image includes ffmpeg, ImageMagick and Ghostscript for video and PDF
thumbnails and runs as uid/gid 1000. The shared folder can be mounted
read-only.

## Configuration

Without `-config` the built-in defaults are used (see
[`web/conf/options.json`](web/conf/options.json), all options are
documented there). A config folder may contain:

- `options.json`: merged over the defaults, so it only needs the options
  you change, e.g.

  ```json
  {
      "passhash": "$2a$12$...",
      "search": {"enabled": true}
  }
  ```

- `types.json`: replaces the default file types
- `l10n/<code>.json`: adds or replaces translations
- `ext/`: files for the `resources` option (extra scripts and styles)

## Security

- Only entries of "managed" folders below the root are listed and
  served. Entries matching `view.hidden` (by default dotfiles and
  `_vitrine*`/`_h5fs*` files) are neither listed nor served.
- Symbolic links are followed only if their target is inside the root.
- Shared HTML, SVG and XML files are served with
  `Content-Security-Policy: sandbox`, PHP files as plain text.
- The cache and config folders are never served, even inside the root.

### Behind a reverse proxy

vitrine only trusts `X-Real-IP`, `X-Forwarded-For` and `X-Forwarded-Proto`
from the addresses given with `-trusted-proxy`. Set it to the address the
proxy connects from, so login throttling and download limits see the real
clients and session cookies get the `Secure` flag behind TLS:

```sh
vitrine -root /srv/share -listen 127.0.0.1:8080 -trusted-proxy 127.0.0.1
```

```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-Proto $scheme;
    proxy_max_temp_file_size 0;
}
```

Don't compress video and other binary responses in the proxy, it breaks
Range requests.

## Migrating from h5fs

- Copy your `_h5fs/private/conf/options.json` into the config folder. Your
  `passhash` keeps working (PHP bcrypt/argon2 and SHA512 hashes).
- Custom header/footer files may keep the `_h5fs.` prefix; `_vitrine.` is
  the new one.
- vitrine serves the files itself: no PHP, no `.htaccess`, no web server
  rules. Point your proxy at vitrine instead of the h5fs `index.php`.
- Changes in behavior:
  - hidden entries are no longer downloadable by direct URL;
  - symbolic links leaving the root are neither listed nor served;
  - packages are built by vitrine, no `tar`/`zip` commands are needed;
    `download.type` is now `tar` or `zip`, the h5fs values `php-tar`,
    `shell-tar` and `shell-zip` still work (as does `foldersize.type`
    `php`, now called `sum`);
  - view preferences stored by h5fs in the browser are taken over when
    vitrine runs on the same origin;
  - thumbnails respect the EXIF orientation of photos;
  - the thumbnail option `thumbnails.exif` (embedded EXIF thumbnails) is
    ignored.

## Development

```sh
go test -race ./...
go run ./cmd/vitrine -root .
```

The frontend sources are in [`frontend/`](frontend) (Node.js 24.18+). The
build output in `web/public` is committed, so rebuild it after changes:

```sh
cd frontend
npm ci
npm run lint && npm test
npm run build   # writes ../web/public
```

## License

MIT, see [LICENSE](LICENSE). The bundled frontend and icons come from h5fs /
h5ai (MIT); some Material Design icons are licensed under CC BY 4.0.
