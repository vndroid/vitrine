import util from '../util/index.js';
import server from '../server.js';
import event from '../core/event.js';
import location from '../core/location.js';
import resource from '../core/resource.js';
import allsettings from '../core/settings.js';
const {each, dom} = util;


const settings = Object.assign({
    enabled: false,
    type: 'tar',
    packageName: 'package',
    alwaysVisible: false
}, allsettings.download);
const tpl =
        `<div id="download" class="tool">
            <img src="${resource.image('download')}" alt="download"/>
        </div>`;
let selectedItems = [];
let $download;


const onSelection = items => {
    selectedItems = items.slice(0);
    if (selectedItems.length) {
        $download.show();
    } else if (!settings.alwaysVisible) {
        $download.hide();
    }
};

const onClick = () => {
    const type = settings.type;
    let name = settings.packageName;
    const extension = type === 'zip' ? 'zip' : 'tar';

    if (!name) {
        if (selectedItems.length === 1) {
            name = selectedItems[0].label;
        } else {
            name = location.getItem().label;
        }
    }

    const query = {
        action: 'download',
        as: name + '.' + extension,
        type,
        baseHref: location.getAbsHref(),
        hrefs: ''
    };

    each(selectedItems, (item, idx) => {
        query[`hrefs[${idx}]`] = item.absHref;
    });

    server.formRequest(query);
};

const init = () => {
    if (!settings.enabled) {
        return;
    }

    $download = dom(tpl)
        .hide()
        .appTo('#toolbar')
        .on('click', onClick);

    if (settings.alwaysVisible) {
        $download.show();
    }

    event.sub('selection', onSelection);
};


init();
