import config from '../config.js';


export default Object.assign({}, config.options, {
    publicHref: config.setup.PUBLIC_HREF,
    assetVersion: config.setup.ASSET_VERSION,
    rootHref: config.setup.ROOT_HREF
});
