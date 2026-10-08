// Resolve the theme before first paint to avoid a light/dark flash.
// Loaded as a classic (blocking) script in <head>, not a module.
// A saved choice wins; otherwise follow the system setting.
(function () {
    var t;
    try { t = localStorage.getItem('txt-theme'); } catch (e) {}
    if (t !== 'light' && t !== 'dark') {
        t = matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
    }
    document.documentElement.dataset.theme = t;
})();
