import scar from 'scar';
import store from '../../../../src/js/lib/core/store.js';

const {test, assert} = scar;

test('core.store takes over h5fs preferences', () => {
    const ls = global.window.localStorage;
    ls.clear();
    ls.setItem('_h5fs', JSON.stringify({view: {mode: 'grid', size: 40}}));

    assert.deep_equal(store.get('view'), {mode: 'grid', size: 40});
    assert.equal(ls.getItem('_h5fs'), null, 'legacy key removed');
    assert.deep_equal(JSON.parse(ls.getItem('_vitrine')), {view: {mode: 'grid', size: 40}});

    store.put('sort', {column: 1});
    assert.deep_equal(JSON.parse(ls.getItem('_vitrine')).sort, {column: 1});
    ls.clear();
});

test('core.store ignores broken data', () => {
    const ls = global.window.localStorage;
    ls.clear();
    ls.setItem('_vitrine', '{broken');
    assert.equal(store.get('view'), undefined);
    store.put('x', 1);
    assert.equal(store.get('x'), 1);
    ls.clear();
});
