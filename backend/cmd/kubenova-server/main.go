// Command kubenova-server is the KubeNova backend. The desktop app launches it
// as a sidecar bound to 127.0.0.1 with a random token; it can also run as a
// shared team server, or as an MCP server over stdio (--mcp).
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/ai"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/api"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/mcp"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/store"
	"github.com/mahmoudkamal448/infrastructue-rebo/backend/internal/tools"
)

func main() {
	listen := flag.String("listen", "127.0.0.1:0", "address to listen on")
	dataDir := flag.String("data-dir", store.DefaultDir(), "application data directory")
	token := flag.String("token", os.Getenv("KUBENOVA_TOKEN"), "API token (random when empty)")
	mcpMode := flag.Bool("mcp", false, "serve KubeNova's Kubernetes tools as an MCP server over stdio")
	mcpCluster := flag.String("cluster", "", "cluster id (or context name) for --mcp mode")
	mcpWrite := flag.Bool("allow-writes", false, "expose mutating tools in --mcp mode")
	version := flag.Bool("version", false, "print version")
	flag.Parse()

	if *version {
		fmt.Println(api.Version)
		return
	}
	tools.FixPath()
	if *token == "" {
		b := make([]byte, 24)
		_, _ = rand.Read(b)
		*token = hex.EncodeToString(b)
	}

	ln, err := net.Listen("tcp", *listen)
	if err != nil {
		log.Fatal(err)
	}
	baseURL := "http://" + ln.Addr().String()
	srv, err := api.New(*dataDir, *token, baseURL)
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if *mcpMode {
		ln.Close()
		log.SetOutput(os.Stderr)
		runMCP(ctx, srv, *mcpCluster, *mcpWrite)
		return
	}

	srv.Start(ctx)
	hs := &http.Server{Handler: srv.Handler(), ReadHeaderTimeout: 15 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	// The Electron main process parses this line to discover port and token.
	ready, _ := json.Marshal(map[string]string{"url": baseURL, "token": *token, "version": api.Version})
	fmt.Printf("KUBENOVA_READY %s\n", ready)
	if err := hs.Serve(ln); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func runMCP(ctx context.Context, srv *api.Server, cluster string, allowWrites bool) {
	id := cluster
	for _, c := range srv.Clusters.List("") {
		if c.ID == cluster || c.Context == cluster || c.Name == cluster {
			id = c.ID
		}
	}
	if id == "" {
		if cs := srv.Clusters.List(""); len(cs) > 0 {
			id = cs[0].ID
		}
	}
	var tools []mcp.ServerTool
	for _, t := range ai.KubernetesTools(ai.Services{Resources: srv.Res, Pods: srv.Pods, Metrics: srv.Metrics, Helm: srv.Helm, GitOps: srv.GitOps}, id, "default") {
		if t.Write && !allowWrites {
			continue
		}
		schema, _ := json.Marshal(map[string]any{"type": "object", "properties": t.Properties, "required": t.Required})
		tools = append(tools, mcp.ServerTool{Tool: mcp.Tool{Name: t.Name, Description: t.Description, InputSchema: schema}, Handler: t.Run})
	}
	if err := mcp.Serve(ctx, os.Stdin, os.Stdout, "kubenova", api.Version, tools); err != nil {
		log.Fatal(err)
	}
}
