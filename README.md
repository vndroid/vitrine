# vitrine

A modern HTML5 directory index for sharing files over the web, shipped as a
single Go binary.

vitrine is a rewrite of [h5fs](https://github.com/vndroid/h5fs), itself a
continuation of [h5ai](https://github.com/lrsjng/h5ai) by Lars Jung. It
replaces the PHP backend (and the web server configuration it depended on)
with a self-contained server that serves the files itself.

> **Status:** work in progress. The backend is being rewritten first and is
> API-compatible with the h5fs frontend, which is bundled unchanged for now.

## License

MIT, see [LICENSE](LICENSE). The bundled frontend and icons come from h5fs /
h5ai (MIT); some Material Design icons are licensed under CC BY 4.0.
