import scar from 'scar';
import util from '../../../../src/js/lib/util/index.js';

const {test, assert} = scar;
const {withVersion} = util;

test('util.withVersion()', () => {
    assert.equal(typeof withVersion, 'function', 'is function');

    assert.equal(withVersion('/_vitrine/public/images/ui/spinner.svg', '0.4.7'), '/_vitrine/public/images/ui/spinner.svg?v=0.4.7');
    assert.equal(withVersion('/a/b.svg', '1.0.0-rc1+x y'), '/a/b.svg?v=1.0.0-rc1%2Bx%20y', 'the version is escaped');
    assert.equal(withVersion('/a/b.svg', ''), '/a/b.svg', 'no version, no parameter');
    assert.equal(withVersion('/a/b.svg', undefined), '/a/b.svg', 'missing version');
});
