import createDOMPurify from 'dompurify';


const purifier = createDOMPurify(global.window);

const sanitize_html = html => purifier.sanitize(html);

export default {
    sanitizeHtml: sanitize_html
};
