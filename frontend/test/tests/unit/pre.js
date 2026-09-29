import fs from 'node:fs';
import {URL} from 'node:url';
import vm from 'node:vm';
import scar from 'scar';

const {test, assert} = scar;
const source = fs.readFileSync(new URL('../../../src/js/pre.js', import.meta.url), 'utf8');

// runs pre.js in a fresh realm without the ES2022 built-ins, like an old browser
const oldBrowser = () => {
    const context = vm.createContext({});
    vm.runInContext(`
        delete Array.prototype.at;
        delete String.prototype.at;
        delete Object.hasOwn;
        this.window = this;
        this.document = {documentElement: {}};
        this.XMLHttpRequest = function () {};
    `, context);
    context.console = console;
    vm.runInContext(source, context);
    return context;
};

test('pre.js adds Array.prototype.at, String.prototype.at and Object.hasOwn', () => {
    const ctx = oldBrowser();
    const run = code => vm.runInContext(code, ctx);
    assert.equal(run('[1, 2, 3].at(0)'), 1);
    assert.equal(run('[1, 2, 3].at(-1)'), 3);
    assert.equal(run('[1, 2, 3].at(3)'), undefined);
    assert.equal(run('[1, 2, 3].at(-4)'), undefined);
    assert.equal(run('[1, 2, 3].at(1.7)'), 2);
    assert.equal(run('"abc".at(-1)'), 'c');
    assert.equal(run('Object.hasOwn({a: 1}, "a")'), true);
    assert.equal(run('Object.hasOwn(Object.create({a: 1}), "a")'), false);
    assert.equal(run('Object.keys(Array.prototype).includes("at")'), false, 'not enumerable');
});

test('pre.js keeps native built-ins', () => {
    const context = vm.createContext({});
    vm.runInContext('this.window = this; this.document = {documentElement: {}}; this.XMLHttpRequest = function () {}; this.nativeAt = Array.prototype.at;', context);
    context.console = console;
    vm.runInContext(source, context);
    assert.ok(vm.runInContext('Array.prototype.at === nativeAt', context));
});
