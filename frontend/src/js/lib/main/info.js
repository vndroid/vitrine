import util from '../util/index.js';
import config from '../config.js';
import server from '../server.js';
const {dom} = util;



const testsTpl =
        '<ul id="tests"></ul>';
const testTpl =
        `<li class="test">
            <span class="label"></span>
            <span class="result"></span>
            <div class="info"></div>
        </li>`;
const loginTpl =
        `<div id="login-wrapper">
            <input id="pass" type="password" placeholder="password"/>
            <span id="login">login</span>
            <span id="logout">logout</span>
            <div id="hint">
                The login is disabled until a password is set:
                put a hash generated with <code>vitrine passwd</code>
                into "passhash" in the <code>options.json</code> of the
                config folder (<code>--config</code>).
            </div>
        </div>`;
const setup = config.setup;


const addTest = (label, info, passed, result) => {
    const $test = dom(testTpl).appTo('#tests');
    $test.find('.label').text(label);
    $test.find('.result')
        .addCls(passed ? 'passed' : 'failed')
        .text(result ? result : passed ? 'yes' : 'no');
    $test.find('.info').html(info);
};

const addTests = () => {
    if (!setup.AS_ADMIN) {
        return;
    }

    dom(testsTpl).appTo('#content');

    addTest(
        'vitrine version', 'Only green if this is an official vitrine release',
        (/^\d+\.\d+\.\d+$/).test(setup.VERSION), setup.VERSION
    );

    addTest(
        'Runtime', 'Go runtime and platform',
        true, setup.GO_VERSION + ' ' + setup.PLATFORM
    );

    addTest(
        'Options parsable', 'File <code>options.json</code> is readable and syntax is correct',
        config.options !== null
    );

    addTest(
        'Types parsable', 'File <code>types.json</code> is readable and syntax is correct',
        config.types !== null
    );

    addTest(
        'Cache directory', 'vitrine has write access to the <code>--cache</code> folder (thumbnails)',
        setup.HAS_WRITABLE_CACHE
    );

    addTest(
        'Movie thumbs', 'Command line program <code>ffmpeg</code> or <code>avconv</code> available',
        setup.HAS_CMD_FFMPEG || setup.HAS_CMD_AVCONV
    );

    addTest(
        'PDF thumbs', 'ImageMagick (<code>magick</code>, <code>convert</code>) or GraphicsMagick (<code>gm</code>) available',
        setup.HAS_CMD_MAGICK || setup.HAS_CMD_CONVERT || setup.HAS_CMD_GM
    );

    addTest(
        'Shell du', 'Command line program <code>du</code> available (folder sizes with type "shell-du")',
        setup.HAS_CMD_DU
    );
};

const reload = () => {
    global.window.location.reload();
};

const onLogin = () => {
    server.request({
        action: 'login',
        pass: dom('#pass').val()
    }).then(reload);
};

const onLogout = () => {
    server.request({
        action: 'logout'
    }).then(reload);
};

const onKeydown = ev => {
    if (ev.which === 13) {
        onLogin();
    }
};

const addLogin = () => {
    dom(loginTpl).appTo('#content');

    if (!setup.AS_ADMIN && !config.options.hasCustomPasshash) {
        dom('#pass').rm();
        dom('#login').rm();
        dom('#logout').rm();
        return;
    }

    dom('#hint').rm();

    if (setup.AS_ADMIN) {
        dom('#pass').rm();
        dom('#login').rm();
        dom('#logout').on('click', onLogout);
    } else {
        dom('#pass').on('keydown', onKeydown)[0].focus();
        dom('#login').on('click', onLogin);
        dom('#logout').rm();
    }
};

const init = () => {
    addLogin();
    addTests();
};


init();
