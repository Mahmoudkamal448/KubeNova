// Package pods implements pod tooling: log streaming (live, previous,
// multi-pod, multi-container, regex filtered), exec, file copy and port
// forwarding.
package pods

import (
	"archive/tar"
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/transport/spdy"

	"github.com/mahmoudkamal448/kubenova/backend/internal/clusters"
)

// Service provides pod tools.
type Service struct {
	Clusters *clusters.Manager
	forwards sync.Map // id -> *Forward
}

// LogLine is a single log line tagged with its origin.
type LogLine struct {
	Pod       string    `json:"pod"`
	Container string    `json:"container"`
	Time      time.Time `json:"time,omitempty"`
	Line      string    `json:"line"`
}

// LogOptions configures log streaming.
type LogOptions struct {
	Namespace     string
	Pods          []string // explicit pods
	LabelSelector string   // or select pods by labels (multi-pod, stern-like)
	Container     string   // empty = all containers
	Follow        bool
	Previous      bool
	TailLines     int64
	SinceSeconds  int64
	Timestamps    bool
	Filter        string // substring or regex
	Regex         bool
}

// StreamLogs streams logs from one or more pods/containers to out until ctx is
// cancelled (or all streams end when not following).
func (s *Service) StreamLogs(ctx context.Context, clusterID string, o LogOptions, out func(LogLine)) error {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return err
	}
	pods := o.Pods
	if o.LabelSelector != "" {
		l, err := typed.CoreV1().Pods(o.Namespace).List(ctx, metav1.ListOptions{LabelSelector: o.LabelSelector})
		if err != nil {
			return err
		}
		for _, p := range l.Items {
			pods = append(pods, p.Name)
		}
	}
	if len(pods) == 0 {
		return fmt.Errorf("no pods selected")
	}
	var matcher func(string) bool
	if o.Filter != "" {
		if o.Regex {
			re, err := regexp.Compile(o.Filter)
			if err != nil {
				return err
			}
			matcher = re.MatchString
		} else {
			f := strings.ToLower(o.Filter)
			matcher = func(s string) bool { return strings.Contains(strings.ToLower(s), f) }
		}
	}
	var mu sync.Mutex
	emit := func(l LogLine) {
		if matcher != nil && !matcher(l.Line) {
			return
		}
		mu.Lock()
		out(l)
		mu.Unlock()
	}
	var wg sync.WaitGroup
	errs := make(chan error, 64)
	for _, podName := range pods {
		pod, err := typed.CoreV1().Pods(o.Namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		var containers []string
		if o.Container != "" {
			containers = []string{o.Container}
		} else {
			for _, c := range pod.Spec.InitContainers {
				containers = append(containers, c.Name)
			}
			for _, c := range pod.Spec.Containers {
				containers = append(containers, c.Name)
			}
		}
		for _, c := range containers {
			wg.Add(1)
			go func(pod, container string) {
				defer wg.Done()
				opts := &corev1.PodLogOptions{Container: container, Follow: o.Follow, Previous: o.Previous, Timestamps: true}
				if o.TailLines > 0 {
					opts.TailLines = &o.TailLines
				}
				if o.SinceSeconds > 0 {
					opts.SinceSeconds = &o.SinceSeconds
				}
				rc, err := typed.CoreV1().Pods(o.Namespace).GetLogs(pod, opts).Stream(ctx)
				if err != nil {
					select {
					case errs <- fmt.Errorf("%s/%s: %w", pod, container, err):
					default:
					}
					return
				}
				defer rc.Close()
				sc := bufio.NewScanner(rc)
				sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
				for sc.Scan() {
					line := sc.Text()
					ll := LogLine{Pod: pod, Container: container, Line: line}
					if i := strings.IndexByte(line, ' '); i > 0 {
						if t, err := time.Parse(time.RFC3339Nano, line[:i]); err == nil {
							ll.Time = t
							if !o.Timestamps {
								ll.Line = line[i+1:]
							}
						}
					}
					emit(ll)
				}
			}(podName, c)
		}
	}
	wg.Wait()
	close(errs)
	if len(pods) == 1 {
		for e := range errs {
			return e
		}
	}
	return nil
}

// TerminalSize is a resize event.
type TerminalSize struct{ Width, Height uint16 }

type sizeQueue struct{ ch chan TerminalSize }

func (q *sizeQueue) Next() *remotecommand.TerminalSize {
	s, ok := <-q.ch
	if !ok {
		return nil
	}
	return &remotecommand.TerminalSize{Width: s.Width, Height: s.Height}
}

// ExecOptions configures exec.
type ExecOptions struct {
	Namespace string
	Pod       string
	Container string
	Command   []string
	TTY       bool
	Stdin     io.Reader
	Stdout    io.Writer
	Stderr    io.Writer
	Resize    chan TerminalSize
}

// Exec runs a command in a container, streaming stdio. It tries WebSocket
// first then falls back to SPDY.
func (s *Service) Exec(ctx context.Context, clusterID string, o ExecOptions) error {
	cfg, err := s.Clusters.Config(clusterID)
	if err != nil {
		return err
	}
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return err
	}
	if len(o.Command) == 0 {
		o.Command = []string{"/bin/sh", "-c", "if command -v bash >/dev/null 2>&1; then exec bash; else exec sh; fi"}
	}
	req := typed.CoreV1().RESTClient().Post().Resource("pods").Name(o.Pod).Namespace(o.Namespace).SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{Container: o.Container, Command: o.Command, Stdin: o.Stdin != nil, Stdout: o.Stdout != nil, Stderr: o.Stderr != nil && !o.TTY, TTY: o.TTY}, scheme.ParameterCodec)
	spdyExec, err := remotecommand.NewSPDYExecutor(cfg, "POST", req.URL())
	if err != nil {
		return err
	}
	exec := spdyExec
	if wsExec, werr := remotecommand.NewWebSocketExecutor(cfg, "GET", req.URL().String()); werr == nil {
		exec, _ = remotecommand.NewFallbackExecutor(wsExec, spdyExec, func(err error) bool { return true })
	}
	so := remotecommand.StreamOptions{Stdin: o.Stdin, Stdout: o.Stdout, Tty: o.TTY}
	if !o.TTY {
		so.Stderr = o.Stderr
	}
	if o.Resize != nil {
		so.TerminalSizeQueue = &sizeQueue{ch: o.Resize}
	}
	return exec.StreamWithContext(ctx, so)
}

// ExecCapture runs a non-interactive command and captures output.
func (s *Service) ExecCapture(ctx context.Context, clusterID, ns, pod, container string, cmd []string, stdin io.Reader) (string, string, error) {
	var stdout, stderr bytes.Buffer
	err := s.Exec(ctx, clusterID, ExecOptions{Namespace: ns, Pod: pod, Container: container, Command: cmd, Stdin: stdin, Stdout: &stdout, Stderr: &stderr})
	return stdout.String(), stderr.String(), err
}

// FileEntry is a directory entry inside a container.
type FileEntry struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	Mode  string `json:"mode"`
	MTime string `json:"mtime"`
}

// ListFiles lists a directory in a container (requires `ls`).
func (s *Service) ListFiles(ctx context.Context, clusterID, ns, pod, container, dir string) ([]FileEntry, error) {
	out, stderr, err := s.ExecCapture(ctx, clusterID, ns, pod, container, []string{"ls", "-la", "--time-style=+%Y-%m-%dT%H:%M", dir}, nil)
	if err != nil {
		// busybox ls does not support --time-style.
		out, stderr, err = s.ExecCapture(ctx, clusterID, ns, pod, container, []string{"ls", "-la", dir}, nil)
		if err != nil {
			return nil, fmt.Errorf("%v: %s", err, stderr)
		}
	}
	var entries []FileEntry
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 || strings.HasPrefix(line, "total") {
			continue
		}
		name := f[len(f)-1]
		if i := strings.Index(line, " -> "); i > 0 {
			name = strings.Fields(line[:i])[len(strings.Fields(line[:i]))-1]
		}
		if name == "." || name == ".." {
			continue
		}
		var size int64
		fmt.Sscan(f[4], &size)
		entries = append(entries, FileEntry{Name: name, Dir: strings.HasPrefix(f[0], "d") || (strings.HasPrefix(f[0], "l") && !strings.Contains(name, ".")), Size: size, Mode: f[0], MTime: f[5]})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Dir != entries[j].Dir {
			return entries[i].Dir
		}
		return entries[i].Name < entries[j].Name
	})
	return entries, nil
}

// Download copies a file out of a container (like kubectl cp) and returns its bytes.
func (s *Service) Download(ctx context.Context, clusterID, ns, pod, container, file string) ([]byte, error) {
	var buf, stderr bytes.Buffer
	err := s.Exec(ctx, clusterID, ExecOptions{Namespace: ns, Pod: pod, Container: container,
		Command: []string{"tar", "cf", "-", "-C", path.Dir(file), path.Base(file)}, Stdout: &buf, Stderr: &stderr})
	if err != nil {
		// Fall back to cat when tar is unavailable.
		out, se, cerr := s.ExecCapture(ctx, clusterID, ns, pod, container, []string{"cat", file}, nil)
		if cerr != nil {
			return nil, fmt.Errorf("%v: %s %s", err, stderr.String(), se)
		}
		return []byte(out), nil
	}
	tr := tar.NewReader(&buf)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, fmt.Errorf("file not found in archive: %w", err)
		}
		if h.Typeflag == tar.TypeReg {
			return io.ReadAll(tr)
		}
	}
}

// Upload copies data into a container at the destination path.
func (s *Service) Upload(ctx context.Context, clusterID, ns, pod, container, dest string, data []byte) error {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: path.Base(dest), Mode: 0o644, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
		return err
	}
	if _, err := tw.Write(data); err != nil {
		return err
	}
	tw.Close()
	_, stderr, err := s.ExecCapture(ctx, clusterID, ns, pod, container, []string{"tar", "xf", "-", "-C", path.Dir(dest)}, &buf)
	if err != nil {
		return fmt.Errorf("%v: %s", err, stderr)
	}
	return nil
}

// Forward is an active port-forward session.
type Forward struct {
	ID         string    `json:"id"`
	ClusterID  string    `json:"clusterId"`
	Namespace  string    `json:"namespace"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	Pod        string    `json:"pod"`
	LocalPort  int       `json:"localPort"`
	RemotePort int       `json:"remotePort"`
	Started    time.Time `json:"started"`
	stop       chan struct{}
}

// StartForward starts forwarding a local port to a pod or service port.
// For services the target pod is resolved through the service selector.
func (s *Service) StartForward(ctx context.Context, clusterID, ns, kind, name string, localPort, remotePort int) (*Forward, error) {
	typed, err := s.Clusters.Typed(clusterID)
	if err != nil {
		return nil, err
	}
	cfg, err := s.Clusters.Config(clusterID)
	if err != nil {
		return nil, err
	}
	pod := name
	if strings.HasPrefix(strings.ToLower(kind), "service") {
		svc, err := typed.CoreV1().Services(ns).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		sel := []string{}
		for k, v := range svc.Spec.Selector {
			sel = append(sel, k+"="+v)
		}
		pl, err := typed.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{LabelSelector: strings.Join(sel, ",")})
		if err != nil {
			return nil, err
		}
		pod = ""
		for _, p := range pl.Items {
			if p.Status.Phase == corev1.PodRunning {
				pod = p.Name
				break
			}
		}
		if pod == "" {
			return nil, fmt.Errorf("no running pods behind service %s", name)
		}
		for _, p := range svc.Spec.Ports {
			if int(p.Port) == remotePort && p.TargetPort.IntValue() > 0 {
				remotePort = p.TargetPort.IntValue()
			}
		}
	}
	if localPort == 0 {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		localPort = l.Addr().(*net.TCPAddr).Port
		l.Close()
	}
	transport, upgrader, err := spdy.RoundTripperFor(cfg)
	if err != nil {
		return nil, err
	}
	url := typed.CoreV1().RESTClient().Post().Resource("pods").Namespace(ns).Name(pod).SubResource("portforward").URL()
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, "POST", url)
	stop := make(chan struct{})
	ready := make(chan struct{})
	pf, err := portforward.NewOnAddresses(dialer, []string{"127.0.0.1"}, []string{fmt.Sprintf("%d:%d", localPort, remotePort)}, stop, ready, io.Discard, io.Discard)
	if err != nil {
		return nil, err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- pf.ForwardPorts() }()
	select {
	case <-ready:
	case err := <-errCh:
		return nil, err
	case <-time.After(15 * time.Second):
		close(stop)
		return nil, fmt.Errorf("timed out starting port-forward")
	}
	f := &Forward{ID: fmt.Sprintf("%s-%s-%s-%d", clusterID, ns, name, localPort), ClusterID: clusterID, Namespace: ns, Kind: kind, Name: name, Pod: pod, LocalPort: localPort, RemotePort: remotePort, Started: time.Now(), stop: stop}
	s.forwards.Store(f.ID, f)
	go func() {
		<-errCh
		s.forwards.Delete(f.ID)
	}()
	return f, nil
}

// StopForward stops a port-forward.
func (s *Service) StopForward(id string) {
	if v, ok := s.forwards.LoadAndDelete(id); ok {
		close(v.(*Forward).stop)
	}
}

// Forwards lists active port-forwards.
func (s *Service) Forwards() []*Forward {
	var out []*Forward
	s.forwards.Range(func(_, v any) bool {
		out = append(out, v.(*Forward))
		return true
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Started.Before(out[j].Started) })
	return out
}
