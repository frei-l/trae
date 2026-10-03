// Starts a real `trae serve` on free ports with a throwaway data directory.
const { spawn, execFileSync } = require("node:child_process");
const fs = require("node:fs");
const net = require("node:net");
const os = require("node:os");
const path = require("node:path");

const root = path.resolve(__dirname, "..");
let binary;

function build() {
  if (binary) return binary;
  binary = path.join(os.tmpdir(), "trae-test-" + process.pid);
  const go = process.env.GO || (fs.existsSync("/usr/local/go/bin/go") ? "/usr/local/go/bin/go" : "go");
  execFileSync(go, ["build", "-o", binary, "."], { cwd: root, stdio: "inherit", env: { ...process.env, CGO_ENABLED: "0" } });
  return binary;
}

function freePort() {
  return new Promise((resolve, reject) => {
    const s = net.createServer();
    s.listen(0, "127.0.0.1", () => {
      const { port } = s.address();
      s.close(() => resolve(port));
    });
    s.on("error", reject);
  });
}

async function startTrae() {
  const bin = build();
  const [otlp, ui] = [await freePort(), await freePort()];
  const data = fs.mkdtempSync(path.join(os.tmpdir(), "trae-data-"));
  const proc = spawn(bin, ["serve", "--otlp", "127.0.0.1:" + otlp, "--ui", "127.0.0.1:" + ui, "--data", data], { stdio: ["ignore", "pipe", "inherit"] });
  const url = "http://127.0.0.1:" + ui;
  const endpoint = "http://127.0.0.1:" + otlp + "/v1/traces";
  for (let i = 0; i < 100; i++) {
    try {
      const r = await fetch(url + "/api/status");
      if (r.ok) break;
    } catch (e) { /* not up yet */ }
    await new Promise((r) => setTimeout(r, 50));
  }
  return {
    url,
    endpoint,
    api: async (p) => (await fetch(url + "/api/" + p)).json(),
    demo: (count) => execFileSync(bin, ["demo", "--endpoint", endpoint, "--count", String(count || 1)]),
    stop: () => { proc.kill(); fs.rmSync(data, { recursive: true, force: true }); },
  };
}

module.exports = { startTrae };
