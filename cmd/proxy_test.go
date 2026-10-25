package cmd

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestProxyAddsBearerToken(t *testing.T) {
	var gotAuth, gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotPath = r.Header.Get("Authorization"), r.URL.Path
	}))
	defer upstream.Close()

	target, _ := url.Parse(upstream.URL + "/mcp/readonly")
	proxy := httptest.NewServer(newProxy(target, "secret"))
	defer proxy.Close()

	req, _ := http.NewRequest("POST", proxy.URL+"/x", nil)
	req.Header.Set("Authorization", "Bearer client")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if gotAuth != "Bearer secret" || gotPath != "/mcp/readonly/x" {
		t.Errorf("upstream got auth %q path %q", gotAuth, gotPath)
	}
}

func TestProxyMissingToken(t *testing.T) {
	t.Setenv("TEFRUN_TEST_TOKEN", "")
	_, err := execute(t, "proxy", "--target", "https://example.com/mcp", "--token-env", "TEFRUN_TEST_TOKEN")
	if err == nil || !strings.Contains(err.Error(), "TEFRUN_TEST_TOKEN is not set") {
		t.Fatalf("got %v", err)
	}
}
