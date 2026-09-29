import util from '../../util/index.js';
import server from '../../server.js';
import allsettings from '../../core/settings.js';
import preview from './preview.js';
const {dom} = util;
const win = global.window;


const settings = Object.assign({
    enabled: false,
    size: null,
    types: []
}, allsettings['preview-img']);
const tpl = '<img id="pv-content-img"/>';

const updateGui = () => {
    const el = dom('#pv-content-img')[0];
    if (!el) {
        return;
    }

    const elW = el.offsetWidth;

    const labels = [preview.item.label];
    if (!settings.size) {
        const elNW = el.naturalWidth;
        const elNH = el.naturalHeight;
        labels.push(String(elNW) + 'x' + String(elNH));
        labels.push(String((100 * elW / elNW).toFixed(0)) + '%');
    }
    preview.setLabels(labels);
};

const requestSample = href => {
    return server.request({
        action: 'get',
        thumbs: [{
            type: 'img',
            href,
            width: settings.size,
            height: 0
        }]
    }).then(json => {
        return json && json.thumbs && json.thumbs[0] ? json.thumbs[0] : null;
    });
};

const load = item => {
    return Promise.resolve(item.absHref)
        .then(href => {
            // without a sample (e.g. too large to scale) show the original
            return settings.size ? requestSample(href).then(sample => sample || href) : href;
        })
        .then(href => new Promise(resolve => {
            const $el = dom(tpl);
            const el = $el[0];
            let timer = null;
            const done = content => {
                if (timer !== null) {
                    win.clearInterval(timer);
                    timer = null;
                    resolve(content);
                }
            };
            // show the image as soon as its size is known, the browser draws
            // the rest while it loads (large or progressive images)
            timer = win.setInterval(() => {
                if (el.naturalWidth > 0) {
                    done($el);
                }
            }, 50);
            $el.on('load', () => done($el))
                .on('error', () => done(preview.unsupported(item)))
                .attr('src', href);
        }));
};

const init = () => {
    if (settings.enabled) {
        preview.register(settings.types, load, updateGui);
    }
};

init();
