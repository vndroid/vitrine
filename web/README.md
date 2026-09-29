# web

Assets embedded into the vitrine binary.

- `public/`: the built frontend (css, js, images). Generated from
  [`frontend/`](../frontend) with `npm run build`; committed so that the Go
  build needs no Node.js. Don't edit it by hand, CI checks that it matches
  the sources.
- `conf/`: default `options.json`, `types.json` and `l10n/` translations,
  taken from h5fs with the documentation adjusted for vitrine. In a config
  directory passed with `--config`, `options.json` is deep merged over the
  defaults (it only needs the changed keys), `types.json` replaces the
  default and `l10n/*.json` files add or replace single languages.
