import util from '../util/index.js';
const {isStr, isFn, dom} = util;


const subscriptions = {};

const sub = (topic, listener) => {
    if (isStr(topic) && isFn(listener)) {
        if (!subscriptions[topic]) {
            subscriptions[topic] = [];
        }
        subscriptions[topic].push(listener);
    }
};

const pub = (topic, ...args) => {
    // console.log(topic, args);
    if (isStr(topic) && subscriptions[topic]) {
        subscriptions[topic].forEach(listener => {
            listener.apply(topic, args);
        });
    }
};

dom(global.window).on('resize', () => pub('resize'));

export default {
    sub,
    pub
};
