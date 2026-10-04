// The UI against a real `trae serve`, in Chrome: light and dark, wide and
// narrow. CHROME points at the browser (default /usr/bin/google-chrome or
// Playwright's own); ARTIFACT_DIR keeps screenshots.
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { chromium } = require("playwright-core");
const { startTrae } = require("./helpers.cjs");

const CHROME = process.env.CHROME || ["/usr/bin/google-chrome", "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"].find((p) => fs.existsSync(p));
const ARTIFACTS = process.env.ARTIFACT_DIR;

let trae, browser;
test.before(async () => {
  trae = await startTrae();
  trae.demo(2);
  browser = await chromium.launch({ executablePath: CHROME, args: ["--no-sandbox"] });
});
test.after(async () => {
  await browser.close();
  trae.stop();
});

async function open(t, opts) {
  const ctx = await browser.newContext({ viewport: { width: opts.width || 1200, height: 800 }, colorScheme: opts.scheme || "light", reducedMotion: "reduce" });
  const page = await ctx.newPage();
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  page.on("console", (m) => { if (m.type() === "error") errors.push(m.text()); });
  await page.goto(trae.url + (opts.path || "/"));
  t.after(async () => {
    assert.deepEqual(errors, [], "page errors");
    await ctx.close();
  });
  return page;
}

async function shot(page, name) {
  if (!ARTIFACTS) return;
  fs.mkdirSync(ARTIFACTS, { recursive: true });
  await page.screenshot({ path: path.join(ARTIFACTS, name + ".png") });
}

for (const scheme of ["light", "dark"]) {
  for (const width of [1200, 800]) {
    test(`traces → trace → span (${scheme}, ${width}px)`, async (t) => {
      const page = await open(t, { scheme, width });
      const rows = page.locator("#tr-list tr.row");
      await rows.first().waitFor();
      assert.equal(await rows.count(), 10);
      assert.equal(await page.locator("#tr-strip .blk").first().locator(".v").textContent(), "10");
      assert.match(await page.locator("#recv").getAttribute("class"), /\bok\b/);
      await shot(page, `traces-${scheme}-${width}`);

      await page.locator("#tr-list tr.row", { hasText: "explain-word" }).first().click();
      await page.locator("#tv-rows .sp").first().waitFor();
      assert.equal(await page.locator("#tv-rows .sp").count(), 4);
      assert.equal(await page.locator("#tv-name").textContent(), "explain-word");
      assert.match(page.url(), /view=trace&id=[0-9a-f]{32}/);
      // The first model call is selected and its conversation shown.
      await page.locator("#tv-detail .cx-role.system").waitFor();
      assert.ok(await page.locator("#tv-detail .cx-role.call").count() >= 1, "tool call shown");
      await shot(page, `trace-${scheme}-${width}`);

      // Keyboard: down to the tool span.
      await page.locator("#tv-rows").focus();
      await page.keyboard.press("ArrowDown");
      assert.equal(await page.locator("#tv-detail h2").textContent(), "lookup_dictionary");
      await page.locator("#tv-detail .segs .opt", { hasText: "Attributes" }).click();
      assert.ok(await page.locator("#tv-detail table.kv tr").count() >= 3);
      await page.locator("#tv-detail .segs .opt", { hasText: "Raw" }).click();
      assert.match(await page.locator("#tv-detail pre.cx-t").textContent(), /"spanId"/);
      await page.locator("#tv-detail .segs .opt", { hasText: "Overview" }).click();

      // Folding the root hides its children.
      await page.locator("#tv-rows .sp").first().locator(".tw").click();
      assert.equal(await page.locator("#tv-rows .sp").count(), 1);

      await page.keyboard.press("Escape");
      await page.locator("#view-traces").waitFor();
      assert.equal(await page.locator("#view-trace").isHidden(), true);
    });
  }
}

test("filters and search", async (t) => {
  const page = await open(t, {});
  await page.locator("#tr-list tr.row").first().waitFor();
  await page.locator("#tr-status .opt", { hasText: "Errors" }).click();
  await page.waitForFunction(() => document.querySelectorAll("#tr-list tr.row").length === 2);
  assert.equal(await page.locator("#tr-list tr.row.bad").count(), 2);
  await page.locator("#tr-status .opt", { hasText: "All" }).click();

  await page.keyboard.press("/");
  await page.keyboard.type("serendipity");
  await page.waitForFunction(() => document.querySelectorAll("#tr-list tr.row").length === 2);

  await page.keyboard.press("Escape");
  await page.locator("#tr-model").click();
  await page.locator(".pop li", { hasText: "gpt-5-mini" }).click();
  await page.waitForFunction(() => document.querySelectorAll("#tr-list tr.row").length === 4);
  assert.equal(await page.locator("#tr-model span").textContent(), "gpt-5-mini");
});

test("live updates add new traces", async (t) => {
  const page = await open(t, {});
  const rows = page.locator("#tr-list tr.row");
  await rows.first().waitFor();
  const before = await rows.count();
  trae.demo(1);
  await page.waitForFunction((n) => document.querySelectorAll("#tr-list tr.row").length === n + 5, before, { timeout: 10000 });
  assert.ok(await page.locator("#tr-list tr.fresh").count() >= 5);

  // Paused, new traces are counted instead of shown.
  await page.locator("#tr-live").click();
  trae.demo(1);
  await page.locator("#tr-live .n").waitFor({ timeout: 10000 });
  assert.equal(await rows.count(), before + 5);
  await page.locator("#tr-live").click();
  await page.waitForFunction((n) => document.querySelectorAll("#tr-list tr.row").length === n + 10, before);
});

test("LLM calls expand in place", async (t) => {
  const page = await open(t, { path: "/?view=calls" });
  const row = page.locator("#cl-list tr.row", { hasText: "refund-helper" }).first();
  await row.waitFor();
  await row.click();
  await page.locator("#cl-list tr.detail .cx-role").first().waitFor();
  assert.equal(await row.getAttribute("aria-expanded"), "true");
  await shot(page, "calls");
  await page.locator("#cl-list tr.detail .link", { hasText: "Open in trace" }).click();
  await page.locator("#tv-rows .sp.on").waitFor();
  assert.match(page.url(), /span=[0-9a-f]{16}/);
});

test("settings changed elsewhere reach the page", async (t) => {
  const page = await open(t, { path: "/?view=settings" });
  await page.locator("#view-settings .pref").first().waitFor();
  // As the app's View menu does: a change that doesn't come from the page.
  await fetch(trae.url + "/api/settings", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ theme: "dark", textSize: 125 }) });
  await page.waitForFunction(() => document.documentElement.dataset.theme === "dark");
  await page.waitForFunction(() => document.documentElement.style.zoom === "1.25");
  await page.locator("#view-settings .opt.on", { hasText: "125%" }).waitFor();
  await fetch(trae.url + "/api/settings", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ theme: "system", textSize: 100 }) });
  await page.waitForFunction(() => !document.documentElement.dataset.theme && !document.documentElement.style.zoom);
});

test("settings: theme, retention, clear", async (t) => {
  const page = await open(t, { path: "/?view=settings" });
  await page.locator("#view-settings .pref").first().waitFor();
  assert.match(await page.locator("#view-settings code").first().textContent(), /\/v1\/traces$/);
  await page.locator("#view-settings .sn-head .opt", { hasText: "Python" }).click();
  assert.match(await page.locator("#view-settings .snippet pre").textContent(), /OTLPSpanExporter/);

  await page.locator("#view-settings .opt", { hasText: "Dark" }).click();
  await page.waitForFunction(() => document.documentElement.dataset.theme === "dark");
  assert.equal((await trae.api("settings")).theme, "dark");
  await page.locator("#view-settings .opt", { hasText: "System" }).click();
  await page.locator("#view-settings .opt", { hasText: "30 days" }).click();
  await page.waitForFunction(async () => (await (await fetch("/api/settings")).json()).retentionDays === 30);
  await shot(page, "settings");

  await page.locator("#view-settings button", { hasText: "Clear all" }).click();
  await page.locator(".dialog button", { hasText: "Clear all" }).click();
  await page.waitForFunction(async () => (await (await fetch("/api/status")).json()).stats.traces === 0);
  await page.locator("#tabs button", { hasText: "Traces" }).click();
  await page.locator("#tr-list .empty-state", { hasText: "Waiting for traces" }).waitFor();
});
