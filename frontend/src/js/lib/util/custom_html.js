import {marked} from 'marked';
import sanitizer from './sanitize_html.js';
const {sanitizeHtml} = sanitizer;


const render_custom_html = (content, type) => {
    const html = type === 'md' ? marked.parse(content) : content;
    return sanitizeHtml(html);
};

export default {
    renderCustomHtml: render_custom_html
};
