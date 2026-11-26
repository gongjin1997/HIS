/* CSP-compliant external theme & density boot script (zero inline script, DESIGN.md §6.5) */
(function () {
  try {
    var html = document.documentElement;
    var theme = localStorage.getItem('kinstall.theme') || 'dark';
    var density = localStorage.getItem('kinstall.density') || 'comfortable';
    var lang = localStorage.getItem('kinstall.lang') || 'zh-CN';
    html.setAttribute('data-theme', theme);
    if (theme === 'dark') {
      html.classList.add('dark');
    } else {
      html.classList.remove('dark');
    }
    html.setAttribute('data-density', density);
    html.setAttribute('lang', lang);
  } catch (_) {
    /* ignore storage errors in restricted contexts */
  }
})();
