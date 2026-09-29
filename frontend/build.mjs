// Builds the frontend into ../web/public, which is embedded into the Go
// binary. The output is committed, so the Go build needs no Node.js; it
// must be deterministic (no dates or commit ids), CI checks that it is
// up to date.
import fs from 'node:fs/promises';
import path from 'node:path';
import {glob} from 'glob';
import * as esbuild from 'esbuild';
import less from 'less';
import postcss from 'postcss';
import autoprefixer from 'autoprefixer';
import cssmin from 'cssmin';
import includeit from './lib/include.js';

const ROOT = import.meta.dirname;
const SRC = path.join(ROOT, 'src');
const OUT = path.resolve(ROOT, '..', 'web', 'public');
const pkg = JSON.parse(await fs.readFile(path.join(ROOT, 'package.json'), 'utf8'));
// no version: the output must only change with the sources
const banner = `/* vitrine frontend - ${pkg.homepage} */\n`;

async function write(dest, content) {
    await fs.mkdir(path.dirname(dest), {recursive: true});
    await fs.writeFile(dest, content);
}

async function bundle(source) {
    const result = await esbuild.build({
        entryPoints: [source],
        bundle: true,
        format: 'iife',
        platform: 'browser',
        target: 'es2020',
        minify: true,
        sourcemap: false,
        define: {global: 'window'},
        legalComments: 'none',
        logOverride: {'unsupported-regexp': 'error'},
        write: false
    });
    return result.outputFiles[0].text;
}

await fs.rm(OUT, {recursive: true, force: true});

// js: pre.js (browser checks, plain script) runs before the bundle
const mainJs = path.join(SRC, 'js', 'scripts.js');
const scripts = includeit({file: mainJs, content: `\n\n// @include "pre.js"\n\n${await bundle(mainJs)}`});
await write(path.join(OUT, 'js', 'scripts.js'), banner + scripts);

// css
for (const source of (await glob('css/*.less', {cwd: SRC, absolute: true})).sort()) {
    let css = includeit({file: source, content: await fs.readFile(source, 'utf8')});
    css = (await less.render(css, {paths: [path.dirname(source)], filename: source, ieCompat: true})).css;
    css = (await postcss([autoprefixer]).process(css, {from: source})).css;
    await write(path.join(OUT, 'css', path.basename(source, '.less') + '.css'), banner + cssmin(css, -1));
}

// images and other static files
for (const source of (await glob('images/**', {cwd: SRC, absolute: true, dot: true, nodir: true})).sort()) {
    if (source.endsWith('.DS_Store')) continue;
    await write(path.join(OUT, path.relative(SRC, source)), await fs.readFile(source));
}

console.log(`built ${path.relative(process.cwd(), OUT) || '.'}`);
