const pw = require("playwright-core");
const { spawn } = require("child_process");
// Usage: node render.cjs full out.mp4   or   node render.cjs preview 3.5,10,20
const [, , mode, out] = process.argv;
(async () => {
  const b = await pw.chromium.launch({ args: ["--no-sandbox"] });
  const p = await b.newPage({ viewport: { width: 1920, height: 1080 } });
  await p.goto("file://" + __dirname + "/index.html", { waitUntil: "domcontentloaded" });
  await p.waitForTimeout(800);
  if (mode === "preview") {
    for (const t of out.split(",")) { await p.evaluate((t) => window.seek(+t), t); await p.screenshot({ path: `${__dirname}/prev-${t}.png` }); }
  } else {
    const fps = 30, n = Math.round((await p.evaluate(() => window.DURATION)) * fps);
    const ff = spawn("ffmpeg", ["-y", "-loglevel", "error", "-f", "image2pipe", "-framerate", String(fps), "-c:v", "mjpeg", "-i", "-", "-c:v", "libx264", "-preset", "slow", "-crf", "18", "-pix_fmt", "yuv420p", "-movflags", "+faststart", out], { stdio: ["pipe", "inherit", "inherit"] });
    for (let i = 0; i < n; i++) {
      await p.evaluate((t) => window.seek(t), i / fps);
      const buf = await p.screenshot({ type: "jpeg", quality: 95 });
      if (!ff.stdin.write(buf)) await new Promise((r) => ff.stdin.once("drain", r));
      if (i % 300 === 0) console.log("frame", i, "/", n);
    }
    ff.stdin.end(); await new Promise((r) => ff.on("close", r));
  }
  await b.close();
})().catch((e) => { console.log("ERR", e.message); process.exit(1); });
