import scar from 'scar';
import format from '../../../../src/js/lib/core/format.js';

const {test, assert} = scar;

test('core.format', () => {
    assert.equal(typeof format, 'object');
    assert.deepEqual(Object.keys(format).sort(), ['setDefaultMetric', 'formatSize', 'setDefaultDateFormat', 'formatDate'].sort());
    assert.equal(typeof format.setDefaultMetric, 'function');
    assert.equal(typeof format.formatSize, 'function');
    assert.equal(typeof format.setDefaultDateFormat, 'function');
    assert.equal(typeof format.formatDate, 'function');
});
