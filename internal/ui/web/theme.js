// Applies the theme and text size before the first paint, so the window
// never flashes the wrong colours.
(function () {
  var p = window.bootPrefs || {};
  var q = new URLSearchParams(location.search).get("theme");
  var theme = q || p.theme || "system";
  if (theme === "light" || theme === "dark") document.documentElement.dataset.theme = theme;
  if (p.textSize && p.textSize !== 100) document.documentElement.style.zoom = p.textSize / 100;
})();
