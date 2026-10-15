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
const build = (arch, dest) => {
  console.log(`building backend for ${goos}/${arch} -> ${dest}`);
  execFileSync("go", ["build", "-trimpath", "-ldflags=-s -w", "-o", dest, "./cmd/kubenova-server"], {
    cwd: path.join(__dirname, "..", "..", "backend"),
    env: { ...env, GOARCH: arch },
    stdio: "inherit",
  });
};
if (goos === "darwin" && !process.env.GOARCH) {
  // One universal binary so both the Intel and Apple Silicon app bundles run it.
  build("amd64", out + "-amd64");
  build("arm64", out + "-arm64");
  execFileSync("lipo", ["-create", "-output", out, out + "-amd64", out + "-arm64"], { stdio: "inherit" });
  require("node:fs").rmSync(out + "-amd64");
  require("node:fs").rmSync(out + "-arm64");
} else {
  build(goarch, out);
}
