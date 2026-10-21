// electron-builder afterPack hook. On Linux, puts a small launcher in front of
// the Electron binary: Electron refuses to start as root unless its sandbox is
// disabled, and AppImages can't use the sandbox where Ubuntu 24.04+ restricts
// unprivileged user namespaces. The launcher adds --no-sandbox only then.
const fs = require("node:fs");
const path = require("node:path");

const launcher = (bin) => `#!/bin/sh
HERE="$(dirname "$(readlink -f "$0")")"
if [ "$(id -u)" = "0" ]; then
  exec "$HERE/${bin}" --no-sandbox "$@"
fi
if [ -n "$APPIMAGE" ] && [ "$(cat /proc/sys/kernel/apparmor_restrict_unprivileged_userns 2>/dev/null)" = "1" ]; then
  exec "$HERE/${bin}" --no-sandbox "$@"
fi
exec "$HERE/${bin}" "$@"
`;

exports.default = async function afterPack(ctx) {
  if (ctx.electronPlatformName !== "linux") return;
  const name = ctx.packager.executableName;
  const exe = path.join(ctx.appOutDir, name);
  const bin = `${name}-bin`;
  fs.renameSync(exe, path.join(ctx.appOutDir, bin));
  fs.writeFileSync(exe, launcher(bin), { mode: 0o755 });
};
