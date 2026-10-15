// Package terminal provides local PTY shells (with kubectl, helm, kustomize,
// stern, k9s pre-configured for the selected cluster) streamed over WebSocket.
package terminal

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

// ControlMessage is sent by the browser terminal.
type ControlMessage struct {
	Type string `json:"type"` // input | resize
	Data string `json:"data,omitempty"`
	Cols uint16 `json:"cols,omitempty"`
	Rows uint16 `json:"rows,omitempty"`
}

// DefaultShell returns the user's shell.
func DefaultShell() string {
	if runtime.GOOS == "windows" {
		if p, err := exec.LookPath("pwsh.exe"); err == nil {
			return p
		}
		return "powershell.exe"
	}
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}

// Run starts command in a PTY and bridges it to the websocket until either side closes.
func Run(conn *websocket.Conn, command string, args []string, env []string, dir string) error {
	cmd := exec.Command(command, args...)
	cmd.Env = append(env, "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Dir = dir
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: 120, Rows: 32})
	if err != nil {
		return err
	}
	defer func() {
		_ = f.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 32*1024)
		for {
			n, err := f.Read(buf)
			if n > 0 {
				if werr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				_ = conn.WriteMessage(websocket.TextMessage, []byte("\r\n[process exited]\r\n"))
				return
			}
		}
	}()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return nil
		}
		var cm ControlMessage
		if json.Unmarshal(msg, &cm) != nil {
			_, _ = f.Write(msg)
			continue
		}
		switch cm.Type {
		case "input":
			_, _ = io.WriteString(f, cm.Data)
		case "resize":
			_ = pty.Setsize(f, &pty.Winsize{Cols: cm.Cols, Rows: cm.Rows})
		}
		select {
		case <-done:
			return nil
		default:
		}
	}
}
