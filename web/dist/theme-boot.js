/* CSP-compliant external theme & density boot script (DESIGN.md v3.0, Light-First default) */
(function () {
  try {
    var html = document.documentElement;
    var rawTheme = localStorage.getItem('ki.theme') || 'light';
    var theme = rawTheme;
    if (rawTheme === 'system') {
      theme = window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    var density = localStorage.getItem('ki.density') || 'comfortable';
    var lang = localStorage.getItem('ki.lang') || 'zh-CN';
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
