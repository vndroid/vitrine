import util from '../util/index.js';
import event from '../core/event.js';
import allsettings from '../core/settings.js';
const {map} = util;


const win = global.window;
const settings = Object.assign({
    enabled: false,
    id: 'G-0000000000' // GA4 测量 ID (格式 G-XXXXXXXXXX)
}, allsettings['google-tag-id']);

const snippet = () => {
    // 新的 GA4 gtag.js 代码段
    const script = win.document.createElement('script');
    script.async = true;
    script.src = `https://www.googletagmanager.com/gtag/js?id=${settings.id}`;
    win.document.head.appendChild(script);

    win.dataLayer = win.dataLayer || [];
    win.gtag = function gtag() {
        win.dataLayer.push(arguments);
    };
    win.gtag('js', new Date());
};

const init = () => {
    if (!settings.enabled) {
        return;
    }

    snippet();

    const domain = win.location.hostname;
    // GA4 配置（可用参数见 https://developers.google.com/analytics/devguides/collection/ga4/reference/config）
    win.gtag('config', settings.id, {
        send_page_view: false, // 禁用自动页面浏览事件
        cookie_domain: domain, // 设置 cookie 域
        cookie_flags: 'SameSite=None; Secure' // 设置 cookie 标志
    });

    event.sub('location.changed', item => {
        const loc = win.location;
        const pagePath = item.absHref;

        // 发送 GA4 页面浏览事件
        win.gtag('event', 'page_view', {
            page_title: map(item.getCrumb(), i => i.label).join(' > '),
            page_location: `${loc.protocol}//${loc.host}${pagePath}`,
            page_path: pagePath
        });
    });
};

init();
