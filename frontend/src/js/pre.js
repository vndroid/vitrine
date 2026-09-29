/* eslint-disable func-names,no-var */
(function (win) {
    if (!win || win.window !== win || !win.document) {
        throw new Error('no-window');
    }

    var no_browser = 'no-browser';
    var doc_el = win.document.documentElement;
    doc_el.className = '';

    function assert(msg, expr) {
        if (!expr) {
            doc_el.className = no_browser;
            throw new Error(no_browser + ': ' + msg);
        }
    }

    function is_fn(x) {
        return typeof x === 'function';
    }

    assert('console', win.console && is_fn(win.console.log));
    assert('assign', win.Object && is_fn(win.Object.assign));
    assert('promise', is_fn(win.Promise));
    // assert('xhr', is_fn(win.XMLHttpRequest)); // is object in safari
    assert('xhr', win.XMLHttpRequest);

    // ES2022 built-ins used by dependencies (marked): the bundle targets
    // ES2020, build.mjs only lets these through since they are added here
    function define(obj, name, fn) {
        if (!obj[name]) {
            Object.defineProperty(obj, name, {value: fn, writable: true, configurable: true});
        }
    }
    function at(n) {
        var len = this.length;
        var i = Math.trunc(n) || 0;
        if (i < 0) {
            i += len;
        }
        return i < 0 || i >= len ? undefined : this[i];
    }
    function stringAt(n) {
        return at.call(String(this), n);
    }
    define(win.Array.prototype, 'at', at);
    define(win.String.prototype, 'at', stringAt);
    define(win.Object, 'hasOwn', (obj, key) => Object.prototype.hasOwnProperty.call(obj, key));
}(this));
/* eslint-enable */
