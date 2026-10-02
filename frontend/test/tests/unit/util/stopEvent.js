import scar from 'scar';
import util from '../../../../src/js/lib/util/index.js';

const {test, assert} = scar;
const {stopEvent} = util;

// An overlay with a text and a button, like the preview. The handler of the
// overlay stops mouse events; "above" counts what reaches the page below.
const setup = keepDefault => {
    const win = global.window;
    const doc = win.document;
    doc.body.innerHTML = `
        <div id="page"><div id="overlay">
            <div id="pv-content-txt"><p id="text">some <b id="bold">text</b></p></div>
            <button id="button">close</button>
        </div></div>`;
    const seen = {above: 0};
    doc.getElementById('page').addEventListener('mousedown', () => {
        seen.above += 1;
    });
    doc.getElementById('overlay').addEventListener('mousedown', ev => stopEvent(ev, keepDefault));
    const fire = id => {
        const ev = new win.MouseEvent('mousedown', {bubbles: true, cancelable: true});
        doc.getElementById(id).dispatchEvent(ev);
        return ev;
    };
    return {fire, seen};
};

test('util.stopEvent() cancels the default action and stops the event', () => {
    assert.equal(typeof stopEvent, 'function', 'is function');

    const {fire, seen} = setup();
    const ev = fire('button');
    assert.equal(ev.defaultPrevented, true, 'default cancelled');
    assert.equal(seen.above, 0, 'does not bubble');

    assert.equal(fire('text').defaultPrevented, true, 'without a selector everything is cancelled');
});

test('util.stopEvent() keeps the default action inside the selector', () => {
    const {fire, seen} = setup('#pv-content-txt');

    ['text', 'bold', 'pv-content-txt'].forEach(id => {
        assert.equal(fire(id).defaultPrevented, false, `${id}: text stays selectable`);
    });
    assert.equal(seen.above, 0, 'the event still does not reach the page below');

    assert.equal(fire('button').defaultPrevented, true, 'outside the selector the default is cancelled');
    assert.equal(fire('overlay').defaultPrevented, true, 'the overlay itself too');
    assert.equal(seen.above, 0, 'nothing reaches the page below');
});
