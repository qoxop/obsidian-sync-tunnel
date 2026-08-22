package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

type fakeConnectivityRunner struct {
	request ConnectivityRequest
	report  ConnectivityReport
}

type staticIPResolver struct {
	addresses []net.IPAddr
	err       error
}

func (r staticIPResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return r.addresses, r.err
}

func (f *fakeConnectivityRunner) Check(_ context.Context, request ConnectivityRequest) ConnectivityReport {
	f.request = request
	return f.report
}

func TestNormalizePublicHealthURL(t *testing.T) {
	normalized, err := normalizePublicHealthURL("https://sync.example.com")
	if err != nil {
		t.Fatalf("normalize URL: %v", err)
	}
	if normalized.String() != "https://sync.example.com/healthz" {
		t.Fatalf("normalized URL=%q", normalized.String())
	}

	invalid := []string{
		"http://sync.example.com",
		"https://127.0.0.1",
		"https://localhost",
		"https://user:password@sync.example.com",
		"https://sync.example.com:8443",
		"https://sync.example.com/private",
		"https://sync.example.com?target=localhost",
	}
	for _, raw := range invalid {
		t.Run(raw, func(t *testing.T) {
			if _, err := normalizePublicHealthURL(raw); err == nil {
				t.Fatal("unsafe URL was accepted")
			}
		})
	}
}

func TestDiagnosticIPClassification(t *testing.T) {
	for _, raw := range []string{"198.18.0.122", "198.19.255.1", "fdfe:dcba:9876::78"} {
		if !isClashFakeIP(net.ParseIP(raw)) {
			t.Fatalf("fake IP not detected: %s", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "100.64.0.1", "192.0.2.1", "2001:db8::1"} {
		if !isBlockedDiagnosticIP(net.ParseIP(raw)) {
			t.Fatalf("blocked IP accepted: %s", raw)
		}
	}
	if isBlockedDiagnosticIP(net.ParseIP("1.1.1.1")) {
		t.Fatal("public IP was blocked")
	}
}

func TestResolveSafePublicHostRejectsUnsafeAddresses(t *testing.T) {
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "198.18.0.1", "2001:db8::1"} {
		t.Run(raw, func(t *testing.T) {
			resolver := staticIPResolver{addresses: []net.IPAddr{{IP: net.ParseIP(raw)}}}
			if _, err := resolveSafePublicHost(context.Background(), resolver, "sync.example.com"); err == nil {
				t.Fatal("unsafe resolved address was accepted")
			}
		})
	}
	resolver := staticIPResolver{addresses: []net.IPAddr{{IP: net.ParseIP("104.21.17.171")}}}
	addresses, err := resolveSafePublicHost(context.Background(), resolver, "sync.example.com")
	if err != nil || len(addresses) != 1 {
		t.Fatalf("public address rejected: addresses=%v err=%v", addresses, err)
	}
}

func TestClassifyPublicHealth(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		access     bool
		wantStatus string
		wantText   string
	}{
		{name: "healthy", status: 200, body: `{"status":"ok","version":"1.0.0"}`, wantStatus: "pass", wantText: "检查通过"},
		{name: "wrong origin", status: 200, body: `<html>other app</html>`, wantStatus: "fail", wantText: "不是 Sync Tunnel"},
		{name: "cloudflare 1033", status: 530, body: `<h1>Error 1033</h1><p>Cloudflare Tunnel error</p>`, wantStatus: "fail", wantText: "1033"},
		{name: "origin unavailable", status: 502, body: "", wantStatus: "fail", wantText: "Origin"},
		{name: "access required", status: 403, body: "", wantStatus: "warning", wantText: "Access"},
		{name: "bad access token", status: 403, body: "", access: true, wantStatus: "warning", wantText: "Access"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			check := classifyPublicHealth(test.status, []byte(test.body), test.access)
			if check.Status != test.wantStatus || !strings.Contains(check.Detail, test.wantText) {
				t.Fatalf("check=%+v", check)
			}
		})
	}
}

func TestDoHResolver(t *testing.T) {
	resolver := &dohResolver{client: &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		body := `{"Status":0,"Answer":[{"type":1,"data":"104.21.17.171"}]}`
		if request.URL.Query().Get("type") == "AAAA" {
			body = `{"Status":0,"Answer":[{"type":28,"data":"2606:4700:3030::6815:11ab"}]}`
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}}
	addresses, err := resolver.LookupIPAddr(context.Background(), "sync.example.com")
	if err != nil || len(addresses) != 2 {
		t.Fatalf("addresses=%v err=%v", addresses, err)
	}
}

func TestConnectivityHandler(t *testing.T) {
	runner := &fakeConnectivityRunner{report: ConnectivityReport{
		CheckedAt: 1,
		Overall:   "healthy",
		Summary:   "ok",
		Checks:    []ConnectivityCheck{},
	}}
	api := &AdminAPI{connectivity: runner}
	body := bytes.NewBufferString(`{"public_url":"https://sync.example.com","access_client_id":"id","access_client_secret":"secret"}`)
	request := httptest.NewRequest(http.MethodPost, "/admin/v1/connectivity/check", body)
	response := httptest.NewRecorder()
	api.checkConnectivity(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if runner.request.PublicURL != "https://sync.example.com" || runner.request.AccessClientSecret != "secret" {
		t.Fatalf("request=%+v", runner.request)
	}
	var report ConnectivityReport
	if err := json.Unmarshal(response.Body.Bytes(), &report); err != nil || report.Overall != "healthy" {
		t.Fatalf("report=%+v err=%v", report, err)
	}

	incomplete := httptest.NewRequest(http.MethodPost, "/admin/v1/connectivity/check", strings.NewReader(`{"public_url":"https://sync.example.com","access_client_id":"id"}`))
	incompleteResponse := httptest.NewRecorder()
	api.checkConnectivity(incompleteResponse, incomplete)
	if incompleteResponse.Code != http.StatusBadRequest {
		t.Fatalf("incomplete credentials status=%d", incompleteResponse.Code)
	}
}
