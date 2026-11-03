import createDOMPurify from 'dompurify';


const purifier = createDOMPurify(global.window);

// Content of the files is not trusted. DOMPurify keeps style elements and
// the id and name attributes by default: a style element restyles the whole
// page, and an id or a name can shadow an element of the page that the script
// looks up by it. Both are dropped. With strict the style attribute goes too,
// since a fixed position can cover the page.
const lenient = {FORBID_TAGS: ['style'], FORBID_ATTR: ['id', 'name']};
const strict = {FORBID_TAGS: ['style'], FORBID_ATTR: ['id', 'name', 'style']};

const sanitize_html = (html, isStrict = false) => purifier.sanitize(html, isStrict ? strict : lenient);

export default {
    sanitizeHtml: sanitize_html
};
