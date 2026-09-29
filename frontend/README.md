# vitrine frontend

The browser frontend of vitrine, continued from the h5fs/h5ai frontend:
plain ES modules bundled with esbuild (target ES2020) and LESS styles.

```sh
npm ci
npm run lint    # eslint, incl. es-x: no built-ins newer than ES2020
npm test        # unit tests (scar + jsdom)
npm run build   # writes ../web/public, which is embedded into the binary
```

- `src/js/`: `scripts.js` is the entry, `pre.js` (browser checks) is
  prepended as a plain script
- `src/css/`: `styles.less`, `// @include` lines are expanded by
  `lib/include.js` before LESS runs
- `src/images/`: icons, themes and fallback images, copied as they are
