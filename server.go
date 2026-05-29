package main

import (
	"context"
	"fmt"
	"net"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"
)

type serverStartedMsg struct {
	url        string // http://localhost:<port>
	networkURL string // http://<LAN IP>:<port>, empty if no LAN IP found
	stop       func()
}

type serverErrorMsg struct{ err error }

// startServerCmd binds the port, starts an HTTP file server, and returns the
// URL and a stop function via serverStartedMsg. Returns serverErrorMsg on failure.
func startServerCmd(outputDir string, port int) tea.Cmd {
	return func() tea.Msg {
		dir := expandHome(outputDir)
		ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			return serverErrorMsg{err: fmt.Errorf("cannot bind port %d: %w", port, err)}
		}
		ctx, cancel := context.WithCancel(context.Background())
		srv := &http.Server{Handler: http.FileServer(http.Dir(dir))}
		go func() {
			<-ctx.Done()
			srv.Close()
		}()
		go srv.Serve(ln) //nolint:errcheck
		netURL := ""
		if ip := localIP(); ip != "" {
			netURL = fmt.Sprintf("http://%s:%d", ip, port)
		}
		return serverStartedMsg{
			url:        fmt.Sprintf("http://localhost:%d", port),
			networkURL: netURL,
			stop:       cancel,
		}
	}
}

// serveBlocking starts an HTTP file server for the configured output directory
// and blocks until the process is interrupted. Used by the --serve CLI flag.
func serveBlocking(cfg Config) error {
	dir := expandHome(cfg.OutputDir)
	fmt.Printf("Serving %s\n\n", dir)
	fmt.Printf("  Local:   http://localhost:%d\n", cfg.ServePort)
	if ip := localIP(); ip != "" {
		fmt.Printf("  Network: http://%s:%d\n", ip, cfg.ServePort)
	}
	fmt.Println("\nPress Ctrl+C to stop.")
	return http.ListenAndServe(fmt.Sprintf(":%d", cfg.ServePort), http.FileServer(http.Dir(dir)))
}

// localIP returns the first non-loopback IPv4 address of the machine,
// or an empty string if none can be found.
func localIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if v4 := ipnet.IP.To4(); v4 != nil {
				return v4.String()
			}
		}
	}
	return ""
}
