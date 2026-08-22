package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	connectivityTimeout = 12 * time.Second
	maxProbeBodyBytes   = 64 * 1024
)

type ConnectivityRequest struct {
	PublicURL          string `json:"public_url"`
	AccessClientID     string `json:"access_client_id"`
	AccessClientSecret string `json:"access_client_secret"`
}

type ConnectivityCheck struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	Status     string `json:"status"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion,omitempty"`
}

type ConnectivityReport struct {
	CheckedAt int64               `json:"checked_at"`
	Overall   string              `json:"overall"`
	Summary   string              `json:"summary"`
	Checks    []ConnectivityCheck `json:"checks"`
}

type connectivityRunner interface {
	Check(context.Context, ConnectivityRequest) ConnectivityReport
}

type ipResolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type connectivityChecker struct {
	localHealthURL  string
	resolver        ipResolver
	publicResolver  ipResolver
	localClient     *http.Client
	connectorClient *http.Client
}

func newConnectivityChecker(localHealthURL string) *connectivityChecker {
	if strings.TrimSpace(localHealthURL) == "" {
		localHealthURL = "http://127.0.0.1:8787/healthz"
	}
	noRedirect := func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &connectivityChecker{
		localHealthURL: localHealthURL,
		resolver:       net.DefaultResolver,
		publicResolver: newDoHResolver(),
		localClient: &http.Client{
			Timeout:       3 * time.Second,
			CheckRedirect: noRedirect,
		},
		connectorClient: &http.Client{
			Timeout:       1500 * time.Millisecond,
			CheckRedirect: noRedirect,
		},
	}
}

func (a *AdminAPI) checkConnectivity(w http.ResponseWriter, r *http.Request) {
	var request ConnectivityRequest
	if !decodeAdminJSON(w, r, &request) {
		return
	}
	request.PublicURL = strings.TrimSpace(request.PublicURL)
	request.AccessClientID = strings.TrimSpace(request.AccessClientID)
	request.AccessClientSecret = strings.TrimSpace(request.AccessClientSecret)
	if len(request.PublicURL) > 2048 {
		writeError(w, http.StatusBadRequest, "invalid_public_url", "public URL is too long")
		return
	}
	if (request.AccessClientID == "") != (request.AccessClientSecret == "") {
		writeError(w, http.StatusBadRequest, "incomplete_access_credentials", "Cloudflare Access client ID and secret must be provided together")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), connectivityTimeout)
	defer cancel()
	writeJSON(w, http.StatusOK, a.connectivity.Check(ctx, request))
}

func (c *connectivityChecker) Check(ctx context.Context, request ConnectivityRequest) ConnectivityReport {
	checks := []ConnectivityCheck{c.checkLocalHealth(ctx)}

	publicURL, err := normalizePublicHealthURL(request.PublicURL)
	if err != nil {
		checks = append(checks, ConnectivityCheck{
			ID: "public_dns", Label: "公网域名", Status: "fail", Detail: err.Error(),
			Suggestion: "请输入插件使用的 HTTPS Server URL，例如 https://sync.example.com。",
		})
		checks = append(checks, c.checkTunnelEdgeDNS(ctx), c.checkConnectorReady(ctx))
		return finishConnectivityReport(checks)
	}

	addresses, err := resolveSafePublicHost(ctx, c.publicResolver, publicURL.Hostname())
	if err != nil {
		checks = append(checks, ConnectivityCheck{
			ID: "public_dns", Label: "公网域名", Status: "fail", Detail: err.Error(),
			Suggestion: "检查域名 DNS、Cloudflare Public Hostname 和本机代理规则。",
		})
	} else {
		checks = append(checks, ConnectivityCheck{
			ID: "public_dns", Label: "公网域名", Status: "pass",
			Detail: fmt.Sprintf("%s 已解析到 %d 个公网地址", publicURL.Hostname(), len(addresses)),
		})
	}

	checks = append(checks, c.checkTunnelEdgeDNS(ctx), c.checkConnectorReady(ctx))
	if err == nil {
		checks = append(checks, c.checkPublicHealth(ctx, publicURL, request))
	}
	return finishConnectivityReport(checks)
}

func (c *connectivityChecker) checkLocalHealth(ctx context.Context) ConnectivityCheck {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.localHealthURL, nil)
	if err != nil {
		return ConnectivityCheck{ID: "local_service", Label: "本地同步服务", Status: "fail", Detail: "本地健康检查地址无效"}
	}
	response, err := c.localClient.Do(request)
	if err != nil {
		return ConnectivityCheck{
			ID: "local_service", Label: "本地同步服务", Status: "fail", Detail: "无法连接同步服务：" + safeNetworkError(err),
			Suggestion: "在 Docker Desktop 中确认 obsidian-sync-server 容器为 healthy。",
		}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProbeBodyBytes))
	var health struct {
		Status string `json:"status"`
	}
	if response.StatusCode == http.StatusOK && json.Unmarshal(body, &health) == nil && health.Status == "ok" {
		return ConnectivityCheck{ID: "local_service", Label: "本地同步服务", Status: "pass", Detail: "同步 API 健康检查通过"}
	}
	return ConnectivityCheck{
		ID: "local_service", Label: "本地同步服务", Status: "fail",
		Detail:     fmt.Sprintf("本地健康检查返回 HTTP %d", response.StatusCode),
		Suggestion: "检查容器状态和运行日志。",
	}
}

func (c *connectivityChecker) checkTunnelEdgeDNS(ctx context.Context) ConnectivityCheck {
	addresses, err := c.resolver.LookupIPAddr(ctx, "region1.v2.argotunnel.com")
	if err != nil || len(addresses) == 0 {
		return ConnectivityCheck{
			ID: "tunnel_dns", Label: "Tunnel 边缘 DNS", Status: "warning", Detail: "无法解析 Cloudflare Tunnel 边缘域名",
			Suggestion: "检查服务器网络和 DNS；如果使用代理，确认 argotunnel.com 可以直连。",
		}
	}
	for _, address := range addresses {
		if isClashFakeIP(address.IP) {
			return ConnectivityCheck{
				ID: "tunnel_dns", Label: "Tunnel 边缘 DNS", Status: "fail", Detail: "检测到 Clash Verge TUN/Fake-IP 地址",
				Suggestion: "在 Clash Verge 全局扩展脚本中将 argotunnel.com 加入 fake-ip-filter，并让 cloudflared.exe 直连。",
			}
		}
	}
	return ConnectivityCheck{ID: "tunnel_dns", Label: "Tunnel 边缘 DNS", Status: "pass", Detail: "Cloudflare Tunnel 边缘域名解析正常"}
}

func (c *connectivityChecker) checkConnectorReady(ctx context.Context) ConnectivityCheck {
	endpoints := []string{
		"http://127.0.0.1:20241/ready",
		"http://host.docker.internal:20241/ready",
	}
	for _, endpoint := range endpoints {
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			continue
		}
		response, err := c.connectorClient.Do(request)
		if err != nil {
			continue
		}
		response.Body.Close()
		if response.StatusCode == http.StatusOK {
			return ConnectivityCheck{ID: "connector_ready", Label: "cloudflared 连接器", Status: "pass", Detail: "连接器 /ready 检查通过"}
		}
		if response.StatusCode == http.StatusServiceUnavailable {
			return ConnectivityCheck{
				ID: "connector_ready", Label: "cloudflared 连接器", Status: "fail", Detail: "连接器进程存在，但 Tunnel 尚未就绪",
				Suggestion: "重启 cloudflared；若启用 Clash Verge TUN，请检查 Fake-IP 和直连规则。",
			}
		}
	}
	return ConnectivityCheck{
		ID: "connector_ready", Label: "cloudflared 连接器", Status: "info",
		Detail: "容器无法直接读取 Windows cloudflared 的本地 /ready 指标，将以公网检查结果为准",
	}
}

func (c *connectivityChecker) checkPublicHealth(ctx context.Context, healthURL *url.URL, request ConnectivityRequest) ConnectivityCheck {
	client := safePublicHTTPClient(c.publicResolver)
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL.String(), nil)
	if err != nil {
		return ConnectivityCheck{ID: "public_health", Label: "公网同步入口", Status: "fail", Detail: "无法创建公网健康检查请求"}
	}
	httpRequest.Header.Set("Accept", "application/json, text/plain;q=0.9")
	httpRequest.Header.Set("User-Agent", "Sync-Tunnel-Admin-Healthcheck/1.0")
	if request.AccessClientID != "" {
		httpRequest.Header.Set("CF-Access-Client-Id", request.AccessClientID)
		httpRequest.Header.Set("CF-Access-Client-Secret", request.AccessClientSecret)
	}
	response, err := client.Do(httpRequest)
	if err != nil {
		return ConnectivityCheck{
			ID: "public_health", Label: "公网同步入口", Status: "fail", Detail: "公网连接失败：" + safeNetworkError(err),
			Suggestion: "检查互联网连接、域名、TLS 证书和 Cloudflare Tunnel 状态。",
		}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, maxProbeBodyBytes))
	return classifyPublicHealth(response.StatusCode, body, request.AccessClientID != "")
}

func normalizePublicHealthURL(raw string) (*url.URL, error) {
	if raw == "" {
		return nil, errors.New("尚未填写公网同步地址")
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return nil, errors.New("公网地址必须是有效的 HTTPS URL")
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("公网地址不能包含凭据、查询参数或片段")
	}
	if parsed.Port() != "" && parsed.Port() != "443" {
		return nil, errors.New("公网地址只能使用 HTTPS 标准端口 443")
	}
	if net.ParseIP(parsed.Hostname()) != nil || strings.EqualFold(parsed.Hostname(), "localhost") {
		return nil, errors.New("公网地址必须使用公开域名，不能使用 IP 或 localhost")
	}
	if parsed.Path != "" && parsed.Path != "/" && parsed.Path != "/healthz" {
		return nil, errors.New("公网地址只能使用站点根路径或 /healthz")
	}
	parsed.Path = "/healthz"
	parsed.RawPath = ""
	return parsed, nil
}

func resolveSafePublicHost(ctx context.Context, resolver ipResolver, hostname string) ([]net.IPAddr, error) {
	addresses, err := resolver.LookupIPAddr(ctx, hostname)
	if err != nil || len(addresses) == 0 {
		return nil, errors.New("公网域名解析失败")
	}
	for _, address := range addresses {
		if isBlockedDiagnosticIP(address.IP) {
			if isClashFakeIP(address.IP) {
				return nil, errors.New("公网域名被解析为 Clash Fake-IP 地址")
			}
			return nil, errors.New("公网域名解析到了本机或私有网络地址，已拒绝探测")
		}
	}
	return addresses, nil
}

func safePublicHTTPClient(resolver ipResolver) *http.Client {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	transport := &http.Transport{
		Proxy:                 nil,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 8 * time.Second,
		IdleConnTimeout:       30 * time.Second,
	}
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		addresses, err := resolveSafePublicHost(ctx, resolver, host)
		if err != nil {
			return nil, err
		}
		var lastErr error
		for _, candidate := range addresses {
			connection, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(candidate.IP.String(), port))
			if dialErr == nil {
				return connection, nil
			}
			lastErr = dialErr
		}
		if lastErr == nil {
			lastErr = errors.New("no public address available")
		}
		return nil, lastErr
	}
	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

type dohResolver struct {
	client *http.Client
}

func newDoHResolver() *dohResolver {
	return &dohResolver{client: &http.Client{
		Transport: &http.Transport{
			Proxy:                 nil,
			ForceAttemptHTTP2:     true,
			TLSHandshakeTimeout:   4 * time.Second,
			ResponseHeaderTimeout: 4 * time.Second,
		},
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}
}

func (r *dohResolver) LookupIPAddr(ctx context.Context, hostname string) ([]net.IPAddr, error) {
	seen := make(map[string]bool)
	addresses := make([]net.IPAddr, 0, 4)
	var lastErr error
	for _, recordType := range []string{"A", "AAAA"} {
		endpoint := "https://1.1.1.1/dns-query?name=" + url.QueryEscape(hostname) + "&type=" + recordType
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			lastErr = err
			continue
		}
		request.Header.Set("Accept", "application/dns-json")
		response, err := r.client.Do(request)
		if err != nil {
			lastErr = err
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(response.Body, maxProbeBodyBytes))
		response.Body.Close()
		if readErr != nil || response.StatusCode != http.StatusOK {
			lastErr = errors.New("public DNS-over-HTTPS request failed")
			continue
		}
		var answer struct {
			Status int `json:"Status"`
			Answer []struct {
				Type int    `json:"type"`
				Data string `json:"data"`
			} `json:"Answer"`
		}
		if json.Unmarshal(body, &answer) != nil || answer.Status != 0 {
			lastErr = errors.New("public DNS-over-HTTPS response was invalid")
			continue
		}
		for _, record := range answer.Answer {
			if record.Type != 1 && record.Type != 28 {
				continue
			}
			ip := net.ParseIP(record.Data)
			if ip == nil || seen[ip.String()] {
				continue
			}
			seen[ip.String()] = true
			addresses = append(addresses, net.IPAddr{IP: ip})
		}
	}
	if len(addresses) == 0 {
		if lastErr == nil {
			lastErr = errors.New("public DNS-over-HTTPS returned no address")
		}
		return nil, lastErr
	}
	return addresses, nil
}

func classifyPublicHealth(status int, body []byte, accessCredentials bool) ConnectivityCheck {
	base := ConnectivityCheck{ID: "public_health", Label: "公网同步入口"}
	lowerBody := strings.ToLower(string(body))
	if status == http.StatusOK {
		var health struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(body, &health) == nil && health.Status == "ok" {
			base.Status = "pass"
			base.Detail = "Cloudflare 公网 /healthz 检查通过"
			return base
		}
		base.Status = "fail"
		base.Detail = "公网地址返回 HTTP 200，但内容不是 Sync Tunnel 健康响应"
		base.Suggestion = "确认 Public Hostname 指向当前 Sync Tunnel 服务。"
		return base
	}
	if status == 530 && (strings.Contains(lowerBody, "1033") || strings.Contains(lowerBody, "tunnel error")) {
		base.Status = "fail"
		base.Detail = "Cloudflare Error 1033：没有可用的 Tunnel 连接器"
		base.Suggestion = "检查 cloudflared 服务；使用 Clash Verge TUN 时应用 argotunnel.com Fake-IP 排除和直连规则。"
		return base
	}
	if status == http.StatusBadGateway || status == http.StatusGatewayTimeout {
		base.Status = "fail"
		base.Detail = fmt.Sprintf("Cloudflare 已接收请求，但 Origin 返回 HTTP %d", status)
		base.Suggestion = "确认 Public Hostname 的 Origin Service 为 http://127.0.0.1:8787，且本地服务正常。"
		return base
	}
	if status == http.StatusUnauthorized || status == http.StatusForbidden || status >= 300 && status < 400 {
		base.Status = "warning"
		base.Detail = fmt.Sprintf("公网入口返回 HTTP %d，可能被 Cloudflare Access 拦截", status)
		if accessCredentials {
			base.Suggestion = "检查 Cloudflare Access Service Token 是否有效并允许访问该应用。"
		} else {
			base.Suggestion = "如已启用 Cloudflare Access，请临时填写 Service Token 后重新检查。"
		}
		return base
	}
	base.Status = "fail"
	base.Detail = fmt.Sprintf("公网健康检查返回 HTTP %d", status)
	base.Suggestion = "查看 Cloudflare Tunnel 状态、Public Hostname 配置和服务端日志。"
	return base
}

func finishConnectivityReport(checks []ConnectivityCheck) ConnectivityReport {
	overall := "healthy"
	summary := "本地服务与公网同步入口均正常"
	for _, check := range checks {
		if check.Status == "fail" {
			overall = "error"
			summary = "检测到需要处理的连接问题"
			break
		}
		if check.Status == "warning" && overall == "healthy" {
			overall = "warning"
			summary = "连接基本可用，但有项目需要确认"
		}
	}
	return ConnectivityReport{CheckedAt: time.Now().UnixMilli(), Overall: overall, Summary: summary, Checks: checks}
}

func isBlockedDiagnosticIP(ip net.IP) bool {
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || isClashFakeIP(ip) {
		return true
	}
	blockedCIDRs := []string{
		"100.64.0.0/10",
		"192.0.2.0/24",
		"198.51.100.0/24",
		"203.0.113.0/24",
		"2001:db8::/32",
	}
	for _, raw := range blockedCIDRs {
		_, network, _ := net.ParseCIDR(raw)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func isClashFakeIP(ip net.IP) bool {
	for _, raw := range []string{"198.18.0.0/15", "fdfe:dcba:9876::/64"} {
		_, network, _ := net.ParseCIDR(raw)
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func safeNetworkError(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "请求超时"
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "请求超时"
	}
	return "网络连接未建立"
}
