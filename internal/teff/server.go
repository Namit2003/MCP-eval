package teff

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	startupTimeout = 60 * time.Second
	healthInterval = 200 * time.Millisecond
	stopTimeout    = 10 * time.Second
)

// Server is a running mcp-tef process with its own temporary database.
type Server struct {
	URL     string
	cmd     *exec.Cmd
	done    chan struct{} // closed when the process exits
	logFile *os.File
	dbDir   string
}

// Start launches mcp-tef from dir on port, writes its output to logPath and
// waits until it reports healthy. The caller must call Stop.
func Start(dir string, port int, logPath string) (*Server, error) {
	// Fail if something already answers on the port; otherwise the health
	// check could pass against the wrong server.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return nil, fmt.Errorf("mcp-tef: port %d is not free: %w", port, err)
	}
	ln.Close()

	s := &Server{URL: fmt.Sprintf("http://127.0.0.1:%d", port), done: make(chan struct{})}
	if err := s.start(dir, port, logPath); err != nil {
		s.Stop()
		return nil, fmt.Errorf("mcp-tef failed to start (log: %s): %w", logPath, err)
	}
	return s, nil
}

func (s *Server) start(dir string, port int, logPath string) error {
	var err error
	if s.dbDir, err = os.MkdirTemp("", "tefrun-db-"); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	if s.logFile, err = os.Create(logPath); err != nil {
		return err
	}

	s.cmd = exec.Command("uv", "run", "python", "-m", "mcp_tef",
		"--tls-enabled=false", "--host", "127.0.0.1", "--port", strconv.Itoa(port))
	s.cmd.Dir = dir
	s.cmd.Env = append(os.Environ(), "DATABASE_URL=sqlite:///"+filepath.Join(s.dbDir, "mcp-tef.db"))
	s.cmd.Stdout = s.logFile
	s.cmd.Stderr = s.logFile
	// uv starts python as a child; a separate process group lets Stop
	// signal both so neither is left running.
	s.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := s.cmd.Start(); err != nil {
		return err
	}
	go func() {
		s.cmd.Wait()
		close(s.done)
	}()
	return waitHealthy(s.URL+"/health", s.done, startupTimeout)
}

// waitHealthy polls url until it returns 200, the process exits (done is
// closed) or timeout passes.
func waitHealthy(url string, done <-chan struct{}, timeout time.Duration) error {
	client := &http.Client{Timeout: time.Second}
	deadline := time.After(timeout)
	tick := time.NewTicker(healthInterval)
	defer tick.Stop()
	for {
		if resp, err := client.Get(url); err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		select {
		case <-done:
			return errors.New("process exited during startup")
		case <-deadline:
			return fmt.Errorf("not healthy after %s", timeout)
		case <-tick.C:
		}
	}
}

// Stop terminates the server, waiting up to stopTimeout before killing it,
// then removes the temporary database. It is safe to call on a server that
// only partly started.
func (s *Server) Stop() {
	if s.cmd != nil && s.cmd.Process != nil {
		group := -s.cmd.Process.Pid
		syscall.Kill(group, syscall.SIGTERM)
		select {
		case <-s.done:
		case <-time.After(stopTimeout):
		}
		// Kill anything still left in the group, including a python child
		// that outlived uv.
		syscall.Kill(group, syscall.SIGKILL)
		<-s.done
	}
	if s.logFile != nil {
		s.logFile.Close()
	}
	if s.dbDir != "" {
		os.RemoveAll(s.dbDir)
	}
}
