import util from '../util/index.js';
import config from '../config.js';
import settings from './settings.js';
const {includes} = util;


const imagesHref = settings.publicHref + 'images/';
const uiHref = imagesHref + 'ui/';
const themesHref = imagesHref + 'themes/';
const defaultThemeHref = themesHref + 'default/';
const defaultIcons = ['file', 'folder', 'folder-page', 'folder-parent', 'ar', 'aud', 'bin', 'img', 'txt', 'vid', 'x'];


const image = id => uiHref + id + '.svg';

const icon = id => {
    const baseId = (id || '').split('-')[0];
    const href = config.theme[id] || config.theme[baseId];

    if (href) {
        return themesHref + href;
    }

    if (includes(defaultIcons, id)) {
        return defaultThemeHref + id + '.svg';
    }

    if (includes(defaultIcons, baseId)) {
        return defaultThemeHref + baseId + '.svg';
    }

    return defaultThemeHref + 'file.svg';
};


export default {
    image,
    icon
};
