import event from '../core/event.js';
import allsettings from '../core/settings.js';


const doc = global.window.document;
const settings = Object.assign({
    enabled: false
}, allsettings.title);

const onLocationChanged = item => {
    const labels = item.getCrumb().map(i => i.label);
    let title = labels.join(' > ');

    if (labels.length > 1) {
        title = labels[labels.length - 1] + ' - ' + title;
    }

    doc.title = title;
};

const init = () => {
    if (!settings.enabled) {
        return;
    }

    event.sub('location.changed', onLocationChanged);
};

init();
