// node render.cjs preview t1,t2,...   |   node render.cjs frames <startFrame> <endFrame> <fps> <outDir>
const pw = require("/tmp/claude-0/-home-user-infrastructue-rebo/8d056544-edbe-5880-b205-6389fca6393d/scratchpad/pw/node_modules/playwright-core");
const fs = require("fs");
const [, , mode, a, b, fpsArg, outDir] = process.argv;
(async () => {
  const br = await pw.chromium.launch({ executablePath: "/opt/pw-browsers/chromium_headless_shell-1194/chrome-linux/headless_shell", args: ["--no-sandbox", "--force-color-profile=srgb"] });
  const VERT = process.env.VERT === "1";
  const p = await br.newPage({ viewport: VERT ? { width: 1080, height: 1920 } : { width: 1920, height: 1080 } });
  const errs = []; p.on("pageerror", (e) => errs.push(e.message)); p.on("console", (m) => { if (m.type() === "error") errs.push(m.text()); });
  await p.goto("file:///tmp/claude-0/-home-user/8d056544-edbe-5880-b205-6389fca6393d/scratchpad/vo/film/index.html" + (VERT ? "#v" : ""), { waitUntil: "domcontentloaded" });
  await p.waitForTimeout(600);
  if (errs.length) console.log("ERRORS:", errs.join(" | "));
  if (mode === "preview") {
    fs.mkdirSync("/tmp/claude-0/-home-user/8d056544-edbe-5880-b205-6389fca6393d/scratchpad/vo/film/prev", { recursive: true });
    for (const t of a.split(",")) { await p.evaluate((t) => { window.seek(t); }, +t); await p.screenshot({ path: "/tmp/claude-0/-home-user/8d056544-edbe-5880-b205-6389fca6393d/scratchpad/vo/film/prev/" + t + ".jpg", type: "jpeg", quality: 85 }); }
  } else {
    const fps = +fpsArg; fs.mkdirSync(outDir, { recursive: true });
    for (let i = +a; i < +b; i++) {
      await p.evaluate((t) => { window.seek(t); }, i / fps);
      await p.screenshot({ path: outDir + "/f" + String(i).padStart(6, "0") + ".jpg", type: "jpeg", quality: 93 });
    }
  }
  if (errs.length) console.log("ERRORS:", errs.join(" | "));
  await br.close();
})().catch((e) => { console.log("ERR", e.message); process.exit(1); });
