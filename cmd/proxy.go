package cmd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"time"

	"github.com/spf13/cobra"
)

func newProxyCmd() *cobra.Command {
	var target, tokenEnv string
	var port, rate int
	cmd := &cobra.Command{
		Use:   "proxy",
		Short: "Forward local MCP requests to a remote MCP server, adding a bearer token",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			u, err := url.Parse(target)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return fmt.Errorf("--target must be an http(s) URL, got %q", target)
			}
			if port < 1 || port > 65535 {
				return fmt.Errorf("--port must be between 1 and 65535, got %d", port)
			}
			if rate < 0 {
				return fmt.Errorf("--rate must be >= 0, got %d", rate)
			}
			token := os.Getenv(tokenEnv)
			if token == "" {
				return fmt.Errorf("environment variable %s is not set", tokenEnv)
			}

			// Listen on loopback only so other machines cannot use the token.
			srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", port), Handler: newProxy(u, token, rate)}
			go func() {
				<-cmd.Context().Done()
				_ = srv.Shutdown(context.Background())
			}()
			fmt.Fprintf(cmd.OutOrStdout(), "proxying http://%s -> %s\n", srv.Addr, target)
			if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&target, "target", "", "remote MCP server URL")
	cmd.Flags().StringVar(&tokenEnv, "token-env", "", "environment variable holding the bearer token")
	cmd.Flags().IntVar(&port, "port", 8080, "local port to listen on")
	cmd.Flags().IntVar(&rate, "rate", 0, "max requests per second to the target (0 = unlimited)")
	_ = cmd.MarkFlagRequired("target")
	_ = cmd.MarkFlagRequired("token-env")
	return cmd
}

// newProxy forwards every request to target, replacing any Authorization
// header with the bearer token. A rate above 0 spaces requests out to at most
// rate per second; waiting requests are delayed, never rejected or retried.
func newProxy(target *url.URL, token string, rate int) http.Handler {
	rp := &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(target)
			r.Out.Header.Set("Authorization", "Bearer "+token)
		},
		FlushInterval: -1, // pass streamed responses through immediately
	}
	if rate == 0 {
		return rp
	}
	tick := time.NewTicker(time.Second / time.Duration(rate)).C
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-tick:
			rp.ServeHTTP(w, r)
		case <-r.Context().Done():
		}
	})
}
