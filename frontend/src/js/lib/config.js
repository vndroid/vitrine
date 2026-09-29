import server from './server.js';

const {request} = server;
const config = {
    _update: query => request(query).then(resp => Object.assign(config, resp))
};

export default config;
