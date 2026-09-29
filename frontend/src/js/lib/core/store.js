const storekey = '_vitrine';
// preferences stored by h5fs on the same origin are taken over once
const legacyStorekey = '_h5fs';

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
    const obj = read(storekey);
    if (obj) {
        return obj;
    }
    const legacy = read(legacyStorekey);
    if (legacy) {
        save(legacy);
        try {
            storage.removeItem(legacyStorekey);
        } catch {/* skip */}
        return legacy;
    }
    return {};
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
