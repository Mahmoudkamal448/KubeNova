// Downloads the helm CLI into resources/bin so installs and upgrades work
// without a separate helm install. The checksum published next to each
// archive is verified. Usage: node bundle-helm.cjs <goos> [goarch]
const { execFileSync } = require("node:child_process");
const crypto = require("node:crypto");
const fs = require("node:fs");
const https = require("node:https");
const http = require("node:http");
const os = require("node:os");
const path = require("node:path");

const VERSION = process.env.HELM_VERSION || "v3.16.3";
const BASE = process.env.HELM_BASE_URL || "https://get.helm.sh";
const binDir = path.join(__dirname, "..", "resources", "bin");

function get(url) {
  return new Promise((resolve, reject) => {
    (url.startsWith("http:") ? http : https).get(url, (res) => {
      if (res.statusCode >= 300 && res.statusCode < 400 && res.headers.location) return resolve(get(new URL(res.headers.location, url).href));
      if (res.statusCode !== 200) return reject(new Error(`GET ${url}: HTTP ${res.statusCode}`));
      const chunks = [];
      res.on("data", (c) => chunks.push(c));
      res.on("end", () => resolve(Buffer.concat(chunks)));
    }).on("error", reject);
  });
}

async function fetchHelm(goos, arch, dest) {
  const ext = goos === "windows" ? "zip" : "tar.gz";
  const name = `helm-${VERSION}-${goos}-${arch}.${ext}`;
  console.log(`bundling ${name} -> ${dest}`);
  const [archive, sum] = await Promise.all([get(`${BASE}/${name}`), get(`${BASE}/${name}.sha256sum`)]);
  const want = sum.toString().trim().split(/\s+/)[0];
  const got = crypto.createHash("sha256").update(archive).digest("hex");
  if (want !== got) throw new Error(`checksum mismatch for ${name}: want ${want}, got ${got}`);
  const tmp = fs.mkdtempSync(path.join(os.tmpdir(), "helm-"));
  const file = path.join(tmp, name);
  fs.writeFileSync(file, archive);
  // bsdtar (macOS, Windows) and GNU tar both extract tar.gz; bsdtar also reads zip.
  execFileSync("tar", ["-xf", file, "-C", tmp], { stdio: "inherit" });
  const exe = goos === "windows" ? "helm.exe" : "helm";
  fs.copyFileSync(path.join(tmp, `${goos}-${arch}`, exe), dest);
  fs.chmodSync(dest, 0o755);
  fs.rmSync(tmp, { recursive: true, force: true });
}

async function main() {
  const goos = process.argv[2];
  const goarch = process.argv[3];
  fs.mkdirSync(binDir, { recursive: true });
  if (goos === "darwin" && !goarch) {
    // Universal binary, like the backend, for the Intel and Apple Silicon builds.
    const out = path.join(binDir, "helm");
    await fetchHelm("darwin", "amd64", out + "-amd64");
    await fetchHelm("darwin", "arm64", out + "-arm64");
    execFileSync("lipo", ["-create", "-output", out, out + "-amd64", out + "-arm64"], { stdio: "inherit" });
    fs.rmSync(out + "-amd64");
    fs.rmSync(out + "-arm64");
  } else {
    await fetchHelm(goos, goarch || "amd64", path.join(binDir, goos === "windows" ? "helm.exe" : "helm"));
  }
}

main().catch((e) => { console.error(e.message); process.exit(1); });
