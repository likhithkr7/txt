// Resolve the theme before first paint to avoid a light/dark flash.
// Loaded as a classic (blocking) script in <head>, not a module.
// A saved choice wins; otherwise the dark slate theme is the default.
(function () {
    var t;
    try { t = localStorage.getItem('txt-theme'); } catch (e) {}
    document.documentElement.dataset.theme = t === 'light' ? 'light' : 'dark';
})();
