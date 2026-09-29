import scar from 'scar';
import util from '../../../../src/js/lib/util/index.js';

const {test, assert} = scar;
const {renderCustomHtml} = util;

test('util.renderCustomHtml()', () => {
    const unsafeUrl = ['java', 'script:alert(3)'].join('');
    const dirty = `<img src="x" onerror="alert(1)"><script>alert(2)</script><a href="${unsafeUrl}">link</a>`;

    const html = renderCustomHtml(dirty, 'html');
    assert.ok(!html.includes('onerror'), 'sanitizes HTML event handlers');
    assert.ok(!html.includes('<script'), 'sanitizes HTML script elements');

    const markdown = renderCustomHtml(`[link](${unsafeUrl})\n\n<script>alert(4)</script>`, 'md');
    assert.ok(!markdown.includes(unsafeUrl), 'sanitizes Markdown URLs');
    assert.ok(!markdown.includes('<script'), 'sanitizes embedded Markdown HTML');
});
