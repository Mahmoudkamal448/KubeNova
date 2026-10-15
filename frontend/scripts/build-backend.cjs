// Cross-compiles the Go backend into resources/bin for packaging.
const { execFileSync } = require("node:child_process");
const path = require("node:path");
const arg = process.argv.find((a) => a.startsWith("--target="));
const goos = arg ? arg.split("=")[1] : process.platform === "win32" ? "windows" : process.platform;
const goarch = process.env.GOARCH || (process.arch === "x64" ? "amd64" : process.arch);
const out = path.join(__dirname, "..", "resources", "bin", "kubenova-server" + (goos === "windows" ? ".exe" : ""));
require("node:fs").mkdirSync(path.dirname(out), { recursive: true });
const env = { ...process.env, GOOS: goos, GOARCH: goarch, CGO_ENABLED: "0" };
// FIPS 140-3 build: set KUBENOVA_FIPS=1 to compile with the Go FIPS module.
if (process.env.KUBENOVA_FIPS === "1") env.GOFIPS140 = "v1.0.0";
console.log(`building backend for ${goos}/${goarch} -> ${out}`);
execFileSync("go", ["build", "-trimpath", "-ldflags=-s -w", "-o", out, "./cmd/kubenova-server"], {
  cwd: path.join(__dirname, "..", "..", "backend"),
  env,
  stdio: "inherit",
});
