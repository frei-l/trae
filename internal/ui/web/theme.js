// Applies the theme and text size before the first paint, so the page
// never flashes the wrong colours. In the app the Go side sets both
// natively (MyGo's Theme and page zoom); the theme is mirrored here so the
// two agree from the first frame.
(function () {
  var p = window.bootPrefs || {};
  var q = new URLSearchParams(location.search).get("theme");
  var theme = q || p.theme || "system";
  if (theme === "light" || theme === "dark") document.documentElement.dataset.theme = theme;
  if (!window.mygo && p.textSize && p.textSize !== 100) document.documentElement.style.zoom = p.textSize / 100;
})();
