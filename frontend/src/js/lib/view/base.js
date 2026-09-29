import util from '../util/index.js';
const {dom} = util;


const SEL_ROOT = 'body';
const TPL_TOPBAR =
        `<div id="topbar">
            <div id="toolbar"></div>
            <div id="flowbar"></div>
        </div>`;
const TPL_MAINROW =
        `<div id="mainrow">
            <div id="content"></div>
        </div>`;

const init = () => {
    const $root = dom(SEL_ROOT)
        .attr('id', 'root')
        .clr()
        .app(TPL_TOPBAR)
        .app(TPL_MAINROW);

    return {
        $root,
        $topbar: $root.find('#topbar'),
        $toolbar: $root.find('#toolbar'),
        $flowbar: $root.find('#flowbar'),
        $mainrow: $root.find('#mainrow'),
        $content: $root.find('#content')
    };
};

export default init();
