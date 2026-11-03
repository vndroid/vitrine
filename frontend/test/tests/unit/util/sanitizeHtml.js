import scar from 'scar';
import util from '../../../../src/js/lib/util/index.js';

const {test, assert} = scar;
const {sanitizeHtml} = util;

test('util.sanitizeHtml()', () => {
    assert.equal(typeof sanitizeHtml, 'function', 'is function');

    const unsafeUrl = ['java', 'script:alert(3)'].join('');
    const dirty = [
        '<img src="image.jpg" onerror="alert(1)">',
        '<script>alert(2)</script>',
        `<a href="${unsafeUrl}">unsafe</a>`,
        '<strong>safe</strong>'
    ].join('');
    const clean = sanitizeHtml(dirty);

    assert.ok(!clean.includes('onerror'), 'removes event handlers');
    assert.ok(!clean.includes('<script'), 'removes script elements');
    assert.ok(!clean.includes(unsafeUrl), 'removes unsafe URLs');
    assert.ok(clean.includes('<strong>safe</strong>'), 'preserves safe markup');
});

test('util.sanitizeHtml() drops style elements, ids and names', () => {
    const dirty = [
        '<p>text</p><style>#view{display:none}</style><p>more</p>',
        '<div id="items" name="pass" class="keep">cover</div>',
        '<svg><style>*{display:none}</style><rect id="x" width="1" height="1"></rect></svg>',
        '<form name="cookie"><input name="getElementById"></form>',
        '<a href="https://example.com/" target="_blank">link</a>'
    ].join('');

    [false, true].forEach(isStrict => {
        const clean = sanitizeHtml(dirty, isStrict);
        const mode = isStrict ? 'strict' : 'lenient';
        assert.ok(!clean.includes('<style'), `${mode}: removes style elements, also in svg`);
        assert.ok(!(/\sid=/).test(clean), `${mode}: removes id attributes`);
        assert.ok(!(/\sname=/).test(clean), `${mode}: removes name attributes`);
        assert.ok(clean.includes('class="keep"'), `${mode}: keeps the class`);
        assert.ok(clean.includes('href="https://example.com/"'), `${mode}: keeps links`);
        assert.ok(!clean.includes('target='), `${mode}: removes the target`);
        assert.ok(clean.includes('<p>text</p>') && clean.includes('<p>more</p>'), `${mode}: keeps the text around a style element`);
    });
});

test('util.sanitizeHtml() drops style attributes only when strict', () => {
    const dirty = '<div style="position:fixed;inset:0;background:red">cover</div><b style="color:red">x</b>';

    const lenient = sanitizeHtml(dirty);
    assert.ok(lenient.includes('style="position:fixed;inset:0;background:red"'), 'lenient keeps inline styles');

    const strict = sanitizeHtml(dirty, true);
    assert.ok(!strict.includes('style'), 'strict removes inline styles');
    assert.ok(strict.includes('cover') && strict.includes('<b>x</b>'), 'strict keeps the content');
});
