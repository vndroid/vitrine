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
