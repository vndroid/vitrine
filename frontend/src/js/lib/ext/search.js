import util from '../util/index.js';
import server from '../server.js';
import event from '../core/event.js';
import location from '../core/location.js';
import resource from '../core/resource.js';
import allsettings from '../core/settings.js';
import Item from '../model/item.js';
import view from '../view/view.js';
const {map, debounce, parsePattern, dom} = util;



const settings = Object.assign({
    enabled: false,
    advanced: false,
    debounceTime: 300,
    ignorecase: true
}, allsettings.search);
const tpl =
        `<div id="search" class="tool">
            <img src="${resource.image('search')}" alt="search"/>
            <input class="l10n_ph-search" type="text" value=""/>
        </div>`;
let inputIsVisible = false;
let prevPattern = '';
let $search;
let $input;


const search = (pattern = '') => {
    if (pattern === prevPattern) {
        return;
    }
    prevPattern = pattern;

    if (!pattern) {
        view.setLocation();
        return;
    }

    $search.addCls('pending');

    server.request({
        action: 'get',
        search: {
            href: location.getAbsHref(),
            pattern,
            ignorecase: settings.ignorecase
        }
    }).then(response => {
        $search.rmCls('pending');
        view.setHint('noMatch');
        view.setItems(map(response.search, item => Item.get(item)));
    });
};

const update = () => {
    if (inputIsVisible) {
        $search.addCls('active');
        $input[0].focus();
        search(parsePattern($input.val(), settings.advanced));
    } else {
        search();
        $search.rmCls('active');
    }
};

const toggle = () => {
    inputIsVisible = !inputIsVisible;
    update();
};

const reset = () => {
    inputIsVisible = false;
    $input.val('');
    update();
};

const init = () => {
    if (!settings.enabled) {
        return;
    }

    $search = dom(tpl).appTo('#toolbar');
    $input = $search.find('input');

    $search.find('img').on('click', toggle);
    $input.on('keyup', debounce(update, settings.debounceTime));
    event.sub('location.changed', reset);
};


init();
