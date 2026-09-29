import scar from 'scar';

const {test, assert} = scar;

test('window is global object', () => {
    assert.ok(global.window);
    assert.equal(global.window, global.window.window);
    assert.ok(global.window.document);
});
