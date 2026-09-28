# web

Assets embedded into the vitrine binary.

- `public/`: the h5fs frontend (built css, js and images), copied unchanged
  from the h5fs build output (`build-node/_h5fs/public`) of
  [vndroid/h5fs@717d880](https://github.com/vndroid/h5fs/commit/717d880f).
  It still uses the h5fs names and will be replaced by a rewritten frontend.
- `conf/`: default `options.json`, `types.json` and `l10n/` translations,
  taken from h5fs with the documentation adjusted for vitrine. In a config
  directory passed with `--config`, `options.json` is deep merged over the
  defaults (it only needs the changed keys), `types.json` replaces the
  default and `l10n/*.json` files add or replace single languages.
