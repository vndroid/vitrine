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
import includeit from './lib/include.js';
import {ESLint} from 'eslint';
import esx from 'eslint-plugin-es-x';

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

// built-ins src/js/pre.js adds for browsers without them
const polyfilled = {
    'es-x/no-array-prototype-at': 0,
    'es-x/no-string-prototype-at': 0,
    'es-x/no-object-hasown': 0
};

// The bundle targets ES2020 and esbuild does not polyfill APIs: fail the
// build if the bundle, including its dependencies, uses newer built-ins.
async function checkES2020(code) {
    const rules = Object.assign({}, ...['es2021', 'es2022', 'es2023', 'es2024', 'es2025', 'es2026', 'esnext']
        .map(version => esx.configs[`flat/no-new-in-${version}`].rules), polyfilled);
    const eslint = new ESLint({
        overrideConfigFile: true,
        overrideConfig: [{
            languageOptions: {ecmaVersion: 2020, sourceType: 'script'},
            plugins: {'es-x': esx},
            rules
        }]
    });
    const [result] = await eslint.lintText(code, {filePath: 'scripts.js'});
    if (result.errorCount) {
        for (const msg of result.messages) {
            console.error(`bundle ${msg.line}:${msg.column} ${msg.message}`);
        }
        throw new Error('the bundle uses JavaScript newer than ES2020');
    }
}

await fs.rm(OUT, {recursive: true, force: true});

// js: pre.js (browser checks, plain script) runs before the bundle
const mainJs = path.join(SRC, 'js', 'scripts.js');
const bundled = await bundle(mainJs);
await checkES2020(bundled);
const scripts = includeit({file: mainJs, content: `\n\n// @include "pre.js"\n\n${bundled}`});
await write(path.join(OUT, 'js', 'scripts.js'), banner + scripts);

// css
for (const source of (await glob('css/*.less', {cwd: SRC, absolute: true})).sort()) {
    let css = includeit({file: source, content: await fs.readFile(source, 'utf8')});
    css = (await less.render(css, {paths: [path.dirname(source)], filename: source})).css;
    css = (await postcss([autoprefixer]).process(css, {from: source})).css;
    const min = await esbuild.transform(css, {loader: 'css', minify: true});
    await write(path.join(OUT, 'css', path.basename(source, '.less') + '.css'), banner + min.code);
}

// images and other static files
for (const source of (await glob('images/**', {cwd: SRC, absolute: true, dot: true, nodir: true})).sort()) {
    if (source.endsWith('.DS_Store')) continue;
    await write(path.join(OUT, path.relative(SRC, source)), await fs.readFile(source));
}

console.log(`built ${path.relative(process.cwd(), OUT) || '.'}`);
