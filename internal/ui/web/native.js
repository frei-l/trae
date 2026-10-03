// The few things trae asks of the window it runs in. In the app MyGo puts
// its runtime on window.mygo; in a browser (trae serve) it is absent and
// these fall back to the web platform.
(function () {
  var m = window.mygo;
  var ua = navigator.userAgent;
  var platform = m && m.platform ? m.platform : /Mac/.test(ua) ? "darwin" : /Win/.test(ua) ? "win32" : "linux";
  window.native = {
    app: !!m,
    platform: platform,
    mac: platform === "darwin",
    setTitle: function (t) { if (m && m.window) m.window.setTitle(t); else document.title = t; },
  };
})();
