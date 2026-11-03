import {marked} from 'marked';
import sanitizer from './sanitize_html.js';
const {sanitizeHtml} = sanitizer;


const render_custom_html = (content, type) => {
    const html = type === 'md' ? marked.parse(content) : content;
    // inline styles are allowed in a custom header or footer of HTML, not in
    // markdown
    return sanitizeHtml(html, type === 'md');
};

export default {
    renderCustomHtml: render_custom_html
};
