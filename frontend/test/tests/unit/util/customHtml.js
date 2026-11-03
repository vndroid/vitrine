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

test('util.renderCustomHtml() keeps inline styles of HTML, not of Markdown', () => {
    const dirty = '<div style="text-align:center" id="x">centered</div><style>body{display:none}</style>';

    const html = renderCustomHtml(dirty, 'html');
    assert.ok(html.includes('style="text-align:center"'), 'a custom HTML header can be styled inline');
    assert.ok(!html.includes('<style') && !html.includes('id="x"'), 'but has no style element and no id');

    const markdown = renderCustomHtml(dirty.replace('centered', 'centered\n'), 'md');
    assert.ok(!markdown.includes('style'), 'Markdown has no styles at all');
    assert.ok(markdown.includes('centered'), 'but keeps its text');
});
