package tools

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// FixPath makes CLIs installed for the user's shell visible to KubeNova.
// Desktop apps started from the macOS Dock/Finder or a Linux launcher get a
// minimal PATH (/usr/bin:/bin:...), so aws, gcloud, kubelogin, az and friends
// installed via Homebrew, pip or SDK installers are "not found", and EKS/GKE/
// AKS kubeconfigs fail because their exec auth plugins can't be started. We
// ask the login shell for its PATH and also add the usual install locations.
// Windows GUI apps already get the full PATH from the registry.
func FixPath() {
	if runtime.GOOS == "windows" {
		return
	}
	var dirs []string
	dirs = append(dirs, filepath.SplitList(os.Getenv("PATH"))...)
	dirs = append(dirs, filepath.SplitList(loginShellPath())...)
	home, _ := os.UserHomeDir()
	for _, d := range []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", "/usr/local/sbin", "/snap/bin",
		filepath.Join(home, ".local", "bin"), filepath.Join(home, "bin"), filepath.Join(home, ".krew", "bin"),
		filepath.Join(home, "google-cloud-sdk", "bin"), filepath.Join(home, ".rd", "bin"), "/usr/local/google-cloud-sdk/bin",
		"/opt/homebrew/share/google-cloud-sdk/bin", "/usr/local/aws-cli"} {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			dirs = append(dirs, d)
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if d != "" && !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	_ = os.Setenv("PATH", strings.Join(out, string(os.PathListSeparator)))
}

// loginShellPath returns $PATH as the user's interactive login shell sees it.
func loginShellPath() string {
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	const mark = "__KUBENOVA_PATH__"
	cmd := exec.CommandContext(ctx, sh, "-ilc", "printf '"+mark+"%s"+mark+"' \"$PATH\"")
	cmd.Stdin = nil
	out, err := cmd.Output()
	if err != nil && len(out) == 0 {
		return ""
	}
	s := string(out)
	i := strings.Index(s, mark)
	j := strings.LastIndex(s, mark)
	if i < 0 || j <= i {
		return ""
	}
	return s[i+len(mark) : j]
}
