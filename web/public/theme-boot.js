/* CSP-compliant external theme & density boot script (DESIGN.md §6.5, zero inline script) */
(function () {
  try {
    var html = document.documentElement;
    var rawTheme = localStorage.getItem('ki.theme') || localStorage.getItem('kinstall.theme') || 'dark';
    var theme = rawTheme;
    if (rawTheme === 'system') {
      theme = window.matchMedia && window.matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark';
    }
    var density = localStorage.getItem('ki.density') || localStorage.getItem('kinstall.density') || 'comfortable';
    var lang = localStorage.getItem('ki.lang') || localStorage.getItem('kinstall.lang') || 'zh-CN';
    html.setAttribute('data-theme', theme);
    if (theme === 'dark') {
      html.classList.add('dark');
    } else {
      html.classList.remove('dark');
    }
    html.setAttribute('data-density', density);
    html.setAttribute('lang', lang);
  } catch (_) {
    /* ignore restricted storage */
  }
})();
