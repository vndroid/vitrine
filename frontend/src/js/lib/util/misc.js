const esc_pattern = sequence => {
    return sequence.replace(/[\-\[\]{}()*+?.,\\$\^|#\s]/g, '\\$&');
};

const parse_pattern = (sequence, advanced) => {
    if (!advanced) {
        return esc_pattern(sequence);
    }

    if (sequence.substr(0, 3) === 're:') {
        return sequence.substr(3);
    }

    return sequence.trim().split(/\s+/).map(part => {
        return part.split('').map(char => esc_pattern(char)).join('.*?');
    }).join('|');
};

// Stops an event: it does not bubble and its default action is cancelled,
// except for targets inside keepDefault (a selector), e.g. text that has to
// stay selectable.
const stop_event = (ev, keepDefault) => {
    ev.stopPropagation();
    const target = ev.target;
    const keep = keepDefault && target && target.closest && target.closest(keepDefault);
    if (!keep) {
        ev.preventDefault();
    }
};

// Adds the version to the address of a static file, so a CDN or a browser
// that caches by address fetches the file again after an upgrade.
const with_version = (href, version) => {
    return version ? `${href}?v=${encodeURIComponent(version)}` : href;
};

export default {
    parsePattern: parse_pattern,
    withVersion: with_version,
    stopEvent: stop_event
};
