// gitview serves a live, read-only grid of commit graphs for the git
// repositories found under a folder.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

//go:embed web
var webFS embed.FS

func main() {
	addr := flag.String("addr", "", "listen address (default: 127.0.0.1 and the Tailscale IPv4, first free port from 7420)")
	limit := flag.Int("n", 400, "commits to show per repository")
	depth := flag.Int("depth", 5, "how deep to look for repositories")
	interval := flag.Duration("interval", 5*time.Second, "browser refresh interval")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: gitview [flags] [folder]\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	root := "."
	if flag.NArg() > 0 {
		root = flag.Arg(0)
	}
	root, err := filepath.Abs(root)
	if err != nil {
		log.Fatal(err)
	}

	cfgPath, err := defaultConfigPath()
	if err != nil {
		log.Fatal(err)
	}
	store, err := loadStore(cfgPath)
	if err != nil {
		log.Fatal(err)
	}

	web, _ := fs.Sub(webFS, "web")
	srv := &server{
		root:     root,
		depth:    *depth,
		limit:    *limit,
		interval: *interval,
		store:    store,
		static:   http.FileServerFS(web),
	}

	var lns []net.Listener
	if *addr != "" {
		ln, err := net.Listen("tcp", *addr)
		if err != nil {
			log.Fatal(err)
		}
		lns = []net.Listener{ln}
	} else if lns, err = listenDefault(firstPort, firstPort+portTries); err != nil {
		log.Fatal(err)
	}

	log.Printf("gitview: %s", root)
	for _, ln := range lns {
		log.Printf("  http://%s", ln.Addr())
	}
	if name := tailscaleName(); name != "" && *addr == "" {
		_, port, _ := net.SplitHostPort(lns[0].Addr().String())
		log.Printf("  http://%s:%s", name, port)
	}

	errc := make(chan error, len(lns))
	for _, ln := range lns {
		go func() { errc <- http.Serve(ln, srv.routes()) }()
	}
	log.Fatal(<-errc)
}

const (
	firstPort = 7420
	portTries = 50
)

// listenDefault listens on localhost and, when present, the Tailscale IPv4,
// so the graphs are reachable over the tailnet but not the local network.
// It takes the first port in [from, to) that is free on all of them, so
// several folders can be served at once.
func listenDefault(from, to int) ([]net.Listener, error) {
	hosts := []string{"127.0.0.1"}
	if ip := tailscaleIP(); ip != "" {
		hosts = append(hosts, ip)
	}
	var lastErr error
	for port := from; port < to; port++ {
		lns, err := listenAll(hosts, strconv.Itoa(port))
		if err == nil {
			return lns, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("no free port in %d-%d: %w", from, to-1, lastErr)
}

func listenAll(hosts []string, port string) ([]net.Listener, error) {
	var lns []net.Listener
	for _, h := range hosts {
		ln, err := net.Listen("tcp", net.JoinHostPort(h, port))
		if err != nil {
			for _, l := range lns {
				l.Close()
			}
			return nil, err
		}
		lns = append(lns, ln)
	}
	return lns, nil
}

func tailscaleIP() string {
	out, err := exec.Command("tailscale", "ip", "-4").Output()
	if err != nil {
		return ""
	}
	if ip := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]); net.ParseIP(ip) != nil {
		return ip
	}
	return ""
}

// tailscaleName is this machine's MagicDNS name, e.g. host.tailnet.ts.net.
func tailscaleName() string {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return ""
	}
	var st struct{ Self struct{ DNSName string } }
	if json.Unmarshal(out, &st) != nil {
		return ""
	}
	return strings.TrimSuffix(st.Self.DNSName, ".")
}
