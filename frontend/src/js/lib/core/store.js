const storekey = '_vitrine';

// localStorage can be missing or throw (private windows, blocked storage),
// preferences are then only kept in memory
const storage = (() => {
    try {
        const s = global.window.localStorage;
        s.getItem(storekey);
        return s;
    } catch {
        return null;
    }
})();
let memory = {};

const read = key => {
    try {
        const obj = JSON.parse(storage.getItem(key));
        return obj && typeof obj === 'object' ? obj : null;
    } catch {
        return null;
    }
};

const save = obj => {
    memory = obj;
    try {
        if (storage) {
            storage.setItem(storekey, JSON.stringify(obj));
        }
    } catch {/* skip */}
};

const load = () => {
    if (!storage) {
        return memory;
    }
    return read(storekey) || {};
};

const put = (key, value) => {
    const obj = load();
    obj[key] = value;
    save(obj);
};

const get = key => load()[key];


export default {
    put,
    get
};
