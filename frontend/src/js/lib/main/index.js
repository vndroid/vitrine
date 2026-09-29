import '../view/viewmode.js';
import '../ext/autorefresh.js';
import '../ext/contextmenu.js';
import '../ext/crumb.js';
import '../ext/custom.js';
import '../ext/download.js';
import '../ext/filter.js';
import '../ext/google-analytics.js';
import '../ext/info.js';
import '../ext/l10n.js';
import '../ext/piwik-analytics.js';
import '../ext/preview/index.js';
import '../ext/search.js';
import '../ext/select.js';
import '../ext/sort.js';
import '../ext/thumbnails.js';
import '../ext/title.js';
import '../ext/tree.js';
import location from '../core/location.js';



const href = global.window.document.location.href;
location.setLocation(href, true);
