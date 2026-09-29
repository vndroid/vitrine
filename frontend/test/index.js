const run = async () => {
    if (!global.window) {
        const {JSDOM} = await import('jsdom');
        global.window = new JSDOM('').window;
    }

    const {default: scar} = await import('scar');
    const {default: pin} = await import('./util/pin.js');

    await import('./tests/premisses.js');
    await import('./tests/unit/core/event.js');
    await import('./tests/unit/core/format.js');
    await import('./tests/unit/util/naturalCmp.js');
    await import('./tests/unit/util/parsePatten.js');
    await import('./tests/unit/util/sanitizeHtml.js');
    await import('./tests/unit/util/customHtml.js');

    pin.pin_html();
    const suite = await scar.test.run({sync: true});
    if (suite.failed_count) throw new Error(`${suite.failed_count} tests failed`);
};

export const testRun = run();
