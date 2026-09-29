import scar from 'scar';
import event from '../../../../src/js/lib/core/event.js';

const {test, assert} = scar;

test('core.event', () => {
    assert.equal(typeof event, 'object', 'is object');
    assert.deepEqual(Object.keys(event).sort(), ['sub', 'pub'].sort());
    assert.equal(typeof event.sub, 'function');
    assert.equal(typeof event.pub, 'function');
});
