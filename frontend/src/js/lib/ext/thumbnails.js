import util from '../util/index.js';
import server from '../server.js';
import event from '../core/event.js';
import allsettings from '../core/settings.js';
import store from '../core/store.js';
import base from '../view/base.js';
import view from '../view/view.js';
const {each, map, includes} = util;


const win = global.window;
const settings = Object.assign({
    enabled: false,
    img: ['img-bmp', 'img-gif', 'img-ico', 'img-jpg', 'img-png'],
    mov: ['vid-avi', 'vid-flv', 'vid-mkv', 'vid-mov', 'vid-mp4', 'vid-mpg', 'vid-webm'],
    doc: ['x-pdf', 'x-ps'],
    delay: 1,
    size: 100,
    exif: false,
    chunksize: 20
}, allsettings.thumbnails);
const infoSettings = Object.assign({enabled: false}, allsettings.info);
const infoStorekey = 'ext/info';

const square = {ratio: 1, key: 'thumbSquare', selector: '.icon.square img'};
const landscape = {ratio: 4 / 3, key: 'thumbRational', selector: '.icon.landscape img'};

// thumbable items currently in the view
const shown = new Set();
// item -> Set of variants already requested, so each thumb is asked for once
const requested = new WeakMap();
let queue = [];
let flushTimeoutId = null;
let requestChain = Promise.resolve();
// items are only requested once they scroll into (or near) the viewport
let observer = null;


const getType = item => {
    if (includes(settings.img, item.type)) {
        return 'img';
    } else if (includes(settings.mov, item.type)) {
        return 'mov';
    } else if (includes(settings.doc, item.type)) {
        return 'doc';
    }
    return null;
};

// "icons" shows the landscape icon, "details" and "grid" the square one;
// the info sidebar shows the landscape thumb of the hovered item
const neededVariants = () => {
    if (view.getMode() === 'icons') {
        return [landscape];
    }
    return infoSettings.enabled && store.get(infoStorekey) ? [square, landscape] : [square];
};

const setThumb = (item, variant, src) => {
    item[variant.key] = src;
    item.$view.find(variant.selector).addCls('thumb').attr('src', src);
};

const missingVariants = item => {
    const done = requested.get(item);
    return neededVariants().filter(variant => !item[variant.key] && !(done && done.has(variant)));
};

const requestQueue = queued => {
    // drop requests for items that left the view while waiting to be sent
    const reqs = queued.filter(req => {
        if (!shown.has(req.item)) {
            requested.get(req.item).delete(req.variant);
            return false;
        }
        return true;
    });
    if (!reqs.length) {
        return Promise.resolve();
    }

    const thumbs = map(reqs, req => {
        return {
            type: req.type,
            href: req.item.absHref,
            width: Math.round(settings.size * req.variant.ratio),
            height: settings.size
        };
    });

    return server.request({
        action: 'get',
        thumbs
    }).then(json => {
        each(reqs, (req, idx) => {
            const src = json && json.thumbs ? json.thumbs[idx] : null;
            if (src && req.item.$view) {
                setThumb(req.item, req.variant, src);
            }
        });
    });
};

const flush = () => {
    const reqs = queue;
    queue = [];

    const chunksize = settings.chunksize;
    for (let i = 0; i < reqs.length; i += chunksize) {
        const chunk = reqs.slice(i, i + chunksize);
        requestChain = requestChain.then(() => requestQueue(chunk));
    }
};

const queueItem = item => {
    const type = getType(item);
    if (!type || !shown.has(item)) {
        return;
    }

    if (!requested.has(item)) {
        requested.set(item, new Set());
    }
    each(missingVariants(item), variant => {
        requested.get(item).add(variant);
        queue.push({type, item, variant});
    });

    if (queue.length && flushTimeoutId === null) {
        flushTimeoutId = win.setTimeout(() => {
            flushTimeoutId = null;
            flush();
        }, settings.delay);
    }
};

const watchItem = item => {
    if (missingVariants(item).length) {
        if (observer) {
            observer.observe(item.$view[0]);
        } else {
            queueItem(item);
        }
    }
};

const onViewChanged = (added, removed) => {
    each(removed || [], item => {
        shown.delete(item);
        if (observer && item.$view) {
            observer.unobserve(item.$view[0]);
        }
    });

    each(added || [], item => {
        if (getType(item)) {
            shown.add(item);
            // thumbs fetched earlier (e.g. before leaving and re-entering the folder)
            each([square, landscape], variant => {
                if (item[variant.key]) {
                    setThumb(item, variant, item[variant.key]);
                }
            });
            watchItem(item);
        }
    });
};

// the needed variants change with the view mode and the info sidebar toggle
const onNeedsChanged = () => {
    shown.forEach(watchItem);
};

const init = () => {
    if (!settings.enabled) {
        return;
    }

    if (win.IntersectionObserver) {
        observer = new win.IntersectionObserver(entries => {
            each(entries, entry => {
                if (entry.isIntersecting) {
                    observer.unobserve(entry.target);
                    queueItem(entry.target._item);
                }
            });
        }, {root: base.$content[0], rootMargin: '200px 0px'});
    }

    event.sub('view.changed', onViewChanged);
    event.sub('view.mode.changed', onNeedsChanged);
    // the info sidebar publishes "resize" when toggled
    event.sub('resize', onNeedsChanged);
};


init();
