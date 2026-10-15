package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/pods"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/terminal"
)

func logOptions(r *http.Request) pods.LogOptions {
	var podList []string
	if p := q(r, "pods"); p != "" {
		podList = strings.Split(p, ",")
	}
	return pods.LogOptions{
		Namespace: q(r, "namespace"), Pods: podList, LabelSelector: q(r, "labelSelector"), Container: q(r, "container"),
		Follow: qb(r, "follow"), Previous: qb(r, "previous"), TailLines: qi(r, "tail", 500), SinceSeconds: qi(r, "since", 0),
		Timestamps: qb(r, "timestamps"), Filter: q(r, "filter"), Regex: qb(r, "regex"),
	}
}

// wsWriter adapts a websocket to io.Writer for exec output.
type wsWriter struct {
	conn *websocket.Conn
	mu   chan struct{}
}

func (w *wsWriter) Write(p []byte) (int, error) {
	w.mu <- struct{}{}
	defer func() { <-w.mu }()
	if err := w.conn.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *Server) podRoutes(mux *http.ServeMux) {
	// Live / previous / multi-pod / multi-container logs over WebSocket, batched.
	mux.HandleFunc("GET /api/clusters/{id}/logs", func(w http.ResponseWriter, r *http.Request) {
		cid, o := r.PathValue("id"), logOptions(r)
		conn, err := s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					cancel()
					return
				}
			}
		}()
		lines := make(chan pods.LogLine, 4096)
		go func() {
			err := s.Pods.StreamLogs(ctx, cid, o, func(l pods.LogLine) {
				select {
				case lines <- l:
				case <-ctx.Done():
				}
			})
			if err != nil {
				_ = conn.WriteJSON(map[string]string{"error": err.Error()})
			}
			close(lines)
		}()
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		var batch []pods.LogLine
		flush := func() bool {
			if len(batch) == 0 {
				return true
			}
			err := conn.WriteJSON(map[string]any{"lines": batch})
			batch = batch[:0]
			return err == nil
		}
		for {
			select {
			case l, more := <-lines:
				if !more {
					flush()
					_ = conn.WriteJSON(map[string]bool{"eof": true})
					return
				}
				batch = append(batch, l)
				if len(batch) >= 500 && !flush() {
					return
				}
			case <-tick.C:
				if !flush() {
					return
				}
			case <-ctx.Done():
				return
			}
		}
	})
	mux.HandleFunc("GET /api/clusters/{id}/logs/download", func(w http.ResponseWriter, r *http.Request) {
		o := logOptions(r)
		o.Follow = false
		if o.TailLines == 500 {
			o.TailLines = 0
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s-logs.txt", strings.Join(o.Pods, "_")))
		err := s.Pods.StreamLogs(r.Context(), r.PathValue("id"), o, func(l pods.LogLine) {
			fmt.Fprintf(w, "%s %s/%s %s\n", l.Time.Format(time.RFC3339Nano), l.Pod, l.Container, l.Line)
		})
		logErr(err)
	})
	// Interactive exec shell.
	mux.HandleFunc("GET /api/clusters/{id}/exec", func(w http.ResponseWriter, r *http.Request) {
		cid := r.PathValue("id")
		conn, err := s.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var cmd []string
		if c := q(r, "command"); c != "" {
			cmd = []string{"/bin/sh", "-c", c}
		}
		stdinR, stdinW := io.Pipe()
		resize := make(chan pods.TerminalSize, 4)
		out := &wsWriter{conn: conn, mu: make(chan struct{}, 1)}
		ctx, cancel := context.WithCancel(r.Context())
		defer cancel()
		go func() {
			defer stdinW.Close()
			defer close(resize)
			for {
				_, msg, err := conn.ReadMessage()
				if err != nil {
					cancel()
					return
				}
				var cm terminal.ControlMessage
				if json.Unmarshal(msg, &cm) != nil {
					_, _ = stdinW.Write(msg)
					continue
				}
				switch cm.Type {
				case "input":
					_, _ = stdinW.Write([]byte(cm.Data))
				case "resize":
					select {
					case resize <- pods.TerminalSize{Width: cm.Cols, Height: cm.Rows}:
					default:
					}
				}
			}
		}()
		err = s.Pods.Exec(ctx, cid, pods.ExecOptions{Namespace: q(r, "namespace"), Pod: q(r, "pod"), Container: q(r, "container"), Command: cmd, TTY: true, Stdin: stdinR, Stdout: out, Resize: resize})
		if err != nil {
			_, _ = out.Write([]byte("\r\n\x1b[31m" + err.Error() + "\x1b[0m\r\n"))
		}
	})
	mux.HandleFunc("GET /api/clusters/{id}/files", func(w http.ResponseWriter, r *http.Request) {
		entries, err := s.Pods.ListFiles(r.Context(), r.PathValue("id"), q(r, "namespace"), q(r, "pod"), q(r, "container"), q(r, "path"))
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, entries)
	})
	mux.HandleFunc("GET /api/clusters/{id}/files/download", func(w http.ResponseWriter, r *http.Request) {
		b, err := s.Pods.Download(r.Context(), r.PathValue("id"), q(r, "namespace"), q(r, "pod"), q(r, "container"), q(r, "path"))
		if err != nil {
			fail(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Disposition", "attachment; filename="+path.Base(q(r, "path")))
		_, _ = w.Write(b)
	})
	mux.HandleFunc("POST /api/clusters/{id}/files/upload", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(256 << 20); err != nil {
			fail(w, err)
			return
		}
		f, hdr, err := r.FormFile("file")
		if err != nil {
			fail(w, err)
			return
		}
		defer f.Close()
		data, _ := io.ReadAll(f)
		dest := r.FormValue("path")
		if strings.HasSuffix(dest, "/") || dest == "" {
			dest = path.Join(dest, hdr.Filename)
		}
		if err := s.Pods.Upload(r.Context(), r.PathValue("id"), r.FormValue("namespace"), r.FormValue("pod"), r.FormValue("container"), dest, data); err != nil {
			fail(w, err)
			return
		}
		ok(w, map[string]string{"path": dest})
	})
	// Port forwarding.
	mux.HandleFunc("GET /api/portforwards", func(w http.ResponseWriter, r *http.Request) { ok(w, s.Pods.Forwards()) })
	mux.HandleFunc("POST /api/clusters/{id}/portforwards", func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Namespace, Kind, Name string
			LocalPort, RemotePort int
		}
		if err := decode(r, &b); err != nil {
			fail(w, err)
			return
		}
		f, err := s.Pods.StartForward(context.Background(), r.PathValue("id"), b.Namespace, b.Kind, b.Name, b.LocalPort, b.RemotePort)
		if err != nil {
			fail(w, err)
			return
		}
		ok(w, f)
	})
	mux.HandleFunc("DELETE /api/portforwards/{fid}", func(w http.ResponseWriter, r *http.Request) {
		s.Pods.StopForward(r.PathValue("fid"))
		ok(w, s.Pods.Forwards())
	})
}
