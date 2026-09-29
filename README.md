# vitrine

A modern HTML5 directory index for sharing files over the web, shipped as a
single Go binary.

Vitrine is a rewrite of [h5ai](https://github.com/lrsjng/h5ai) by Lars Jung. It
replaces the PHP backend, and the web server configuration it depended on,
with a self-contained server that serves the files itself.

## Features

- browse folders in details, grid and icon views, with a tree, breadcrumb,
  sorting, filter and search
- previews for images, audio, video (streamed with HTTP Range), text,
  Markdown and source code
- thumbnails for images (JPEG, PNG, GIF, BMP, WebP; AVIF and HEIC with
  ImageMagick; EXIF orientation aware), videos (ffmpeg) and PDF/PostScript
  (ImageMagick), cached with a size limit
- packaged downloads as tar or zip, streamed with limits
- custom header and footer per folder, 35 languages, QR codes
- admin page at `/-/admin` with login (bcrypt, rate limited)

## Run

```sh
go install github.com/vndroid/vitrine/cmd/vitrine@latest
vitrine -root /srv/share
```

Then open http://localhost:8080/.

| Flag | Environment | Default | |
| --- | --- | --- | --- |
| `-root` | `VITRINE_ROOT` | | folder to share (required) |
| `-listen` | `VITRINE_LISTEN` | `:8080` | listen address |
| `-config` | `VITRINE_CONFIG` | | config folder, see below |
| `-cache` | `VITRINE_CACHE` | user cache dir | thumbnail cache |
| `-base-path` | `VITRINE_BASE_PATH` | | URL path to serve below, e.g. `/files` |
| `-trusted-proxy` | `VITRINE_TRUSTED_PROXY` | | reverse proxies, see below |
| `-follow-symlinks` | `VITRINE_FOLLOW_SYMLINKS` | `false` | also serve links whose target is outside the root |
| `-access-log` | `VITRINE_ACCESS_LOG` | `false` | log every request (client, method, path, status, bytes, duration) |
| `-log-format` | `VITRINE_LOG_FORMAT` | `text` | `text` or `json` |

`vitrine -h` (or `--help`) lists all commands and flags with their
environment variables. `vitrine validate -config <dir>` checks a config
folder (exit code 1 on errors, `-strict` also on warnings), `vitrine
passwd` prints a password hash for the admin login, `vitrine -v` (or
`--version`) the version and build information:

```
vitrine, version 0.2.1 (branch: main, revision: 74661efee79c35ce052924b00690e45d363e4913)
  Built: 2026-09-29T08:00:24Z
  Platform: linux/amd64
  Runtime: go1.26.1
  Tags: netgo
```

Build information is set with `-ldflags "-X main.revision=... -X
main.branch=... -X main.buildDate=..."`; without it, `go build` in a git
checkout still records the revision.

### Docker

Multi-arch images (linux/amd64, linux/arm64) are published to the GitHub
Container Registry for tagged releases:

```sh
docker pull ghcr.io/vndroid/vitrine:latest   # or a version, e.g. :0.3.1
```

To build it yourself:

```sh
docker build -t vitrine .
docker run -d -p 8080:8080 \
  -v /srv/share:/share:ro \
  -v /srv/vitrine-config:/config:ro -e VITRINE_CONFIG=/config \
  -v vitrine-cache:/cache \
  vitrine
```

The image includes ffmpeg, ImageMagick and Ghostscript for video and PDF
thumbnails. The shared folder can be mounted read-only.

vitrine runs as `vitrine` (uid/gid 1000, like the first user of most
hosts, so mounted config files may stay private with mode 0600). The user
`www-data` (uid/gid 82) exists too and `vitrine` is a member of its group,
so files owned by either are readable when they are group or world
readable, and `/cache` is writable for both. Files that only their
owner may read (0600) are not readable by the other one. To run as
`www-data` instead, use `--user www-data`.

Arguments are passed to vitrine: flags (`-access-log`) and the commands
`validate`, `passwd`, `version` and `help` work as in
`docker run --rm vitrine validate`; any other command is run as it is.

## Configuration

Without `-config` the built-in defaults are used (see
[`web/conf/options.json`](web/conf/options.json), all options are
documented there). The config folder may contain:

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

Changes to these files apply without a restart: vitrine checks the config
folder every 2 seconds, `SIGHUP` (`docker kill -s HUP <container>`)
reloads at once. A file with errors is logged and the previous config
stays active.

### Admin password

The admin login of the admin page (`/-/admin`) is off until `passhash` is
set in `options.json`. Use a bcrypt hash; `vitrine passwd` asks for the password
twice (without echo) and prints the hash:

```sh
vitrine passwd
# in the Docker image:
docker run --rm -it --entrypoint vitrine vitrine passwd
```

Put the printed `$2a$12$...` string into `options.json`:

```json
{"passhash": "$2a$12$..."}
```

Then run `vitrine validate -config <dir>` to check it. Notes:

- Passwords are limited to 72 bytes, the limit of bcrypt.
- The hash is a plain JSON string in `options.json`. Quote it with single
  quotes if you set it from a shell, it contains `$`.
- Hashes made by PHP's `password_hash()` (bcrypt, argon2) are accepted
  too. Unsalted SHA512 hex digests still work but are weak, replace them
  with a bcrypt hash.
- After 5 failed logins within 15 minutes a client is locked out for 15
  minutes; behind a proxy set `-trusted-proxy`, see below.

## Security

- The URL prefix `/-/` belongs to vitrine (the admin page at `/-/admin`,
  below the `-base-path` if set). `-` is a reserved name: an entry called
  `-` in the root of the shared folder is always hidden, is not served
  and can't be downloaded, whatever `view.hidden` says. A `-` in a
  subfolder is a normal entry.
- Only entries of "managed" folders below the root are listed and
  served. Entries matching `view.hidden` (by default dotfiles and
  `_vitrine*` files) are neither listed nor served.
- Symbolic links are followed only if their target is inside the root,
  unless `-follow-symlinks` is set. The hidden rules always apply to the
  path below the root and, for targets inside the root, to the target;
  links to the cache or config folder are never served.
- Shared HTML, SVG and XML files are served with
  `Content-Security-Policy: sandbox`, PHP files as plain text.
- The cache and config folders are never served, even inside the root.

### Behind a reverse proxy

Vitrine only trusts `X-Real-IP`, `X-Forwarded-For` and `X-Forwarded-Proto`
from the addresses given with `-trusted-proxy`. Set it to the address the
proxy connects from, so login throttling and download limits see the real
clients and session cookies get the `Secure` flag behind TLS:

```sh
vitrine -root /srv/share -listen 127.0.0.1:8080 -trusted-proxy 127.0.0.1
```

#### nginx

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

To serve the vitrine below a path of an existing site, start it with
`-base-path /files` and pass the path on unchanged:

```nginx
absolute_redirect off;  # nginx's own /files -> /files/ redirect keeps the port

location /files/ {
    proxy_pass http://127.0.0.1:8080;   # no URI part: keep /files/
    # proxy_set_header ... as above
}
```

#### Caddy

```caddy
files.example.com {
    reverse_proxy 127.0.0.1:8080 {
        # Caddy sets X-Forwarded-For and -Proto itself, but passes a
        # client's own X-Real-IP on: overwrite it
        header_up X-Real-IP {client_ip}
    }
}
```

Caddy gets the TLS certificate and sets `X-Forwarded-Proto`. `encode`
may be used: Caddy only compresses text responses, so Range requests for
media keep working.

Below a path, with `-base-path /files`; use `handle`, not `handle_path`,
which would strip the path vitrine expects:

```caddy
example.com {
    redir /files /files/ permanent
    handle /files/* {
        reverse_proxy 127.0.0.1:8080 {
            header_up X-Real-IP {client_ip}
        }
    }
}
```

If Caddy itself is behind another proxy or a CDN, list those in Caddy's
`trusted_proxies` server option, so `{client_ip}` is the visitor.

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

MIT, see [LICENSE](LICENSE). The bundled frontend and icons come from h5ai (MIT); some Material Design icons are licensed under CC BY 4.0.
