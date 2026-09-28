# web

Assets embedded into the vitrine binary.

- `public/`: the h5fs frontend (built css, js and images), copied unchanged
  from the h5fs build output (`build-node/_h5fs/public`) of
  [vndroid/h5fs@717d880](https://github.com/vndroid/h5fs/commit/717d880f).
  It still uses the h5fs names and will be replaced by a rewritten frontend.
- `conf/`: default `options.json`, `types.json` and `l10n/` translations,
  taken from h5fs with the documentation adjusted for vitrine. A config
  directory passed with `--config` overrides these files.
