package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

type EgressIPInfo struct {
	IP      string `json:"ip"`
	City    string `json:"city"`
	Region  string `json:"region"`
	Country string `json:"country"`
	Org     string `json:"org"`
}

type UserAgentProbeResult struct {
	UserAgentName string
	UserAgentVal  string
	StatusCode    int
	Server        string
	CloudFrontPop string
	CloudFrontID  string
	Via           string
	ContentType   string
	BodySnippet   string
	OriginReached bool
}

func getEgressIPInfo(ctx context.Context) EgressIPInfo {
	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://ipinfo.io/json", nil)
	if err != nil {
		return EgressIPInfo{IP: "unknown"}
	}
	req.Header.Set("User-Agent", "curl/8.4.0")

	resp, err := client.Do(req)
	if err != nil {
		return EgressIPInfo{IP: fmt.Sprintf("lookup-err: %v", err)}
	}
	defer resp.Body.Close()

	var info EgressIPInfo
	_ = json.NewDecoder(resp.Body).Decode(&info)
	return info
}

// TestFuelFinderBlockDiagnosis executes live probes against the official Fuel Finder API
// with multiple distinct User-Agent headers to determine if blocking is caused by
// User-Agent filtering or by IP CIDR / ASN / Geo-restriction at AWS CloudFront WAF.
func TestFuelFinderBlockDiagnosis(t *testing.T) {
	if os.Getenv("RUN_LIVE_FUEL_FINDER_TEST") != "1" && testing.Short() {
		t.Skip("Skipping live Fuel Finder diagnostic test in short mode. Set RUN_LIVE_FUEL_FINDER_TEST=1 to run.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	ipInfo := getEgressIPInfo(ctx)
	t.Logf("================================================================================")
	t.Logf("FUEL FINDER LIVE DIAGNOSTIC PROBE")
	t.Logf("Egress IP:       %s", ipInfo.IP)
	t.Logf("Organization/ASN: %s", ipInfo.Org)
	t.Logf("Location:        %s, %s, %s", ipInfo.City, ipInfo.Region, ipInfo.Country)
	t.Logf("================================================================================")

	targetURL := "https://www.fuel-finder.service.gov.uk/api/v1/oauth/generate_access_token"
	if override := os.Getenv("FUEL_FINDER_TOKEN_URL"); override != "" {
		targetURL = override
	}

	clientID := os.Getenv("FUEL_FINDER_CLIENT_ID")
	if clientID == "" {
		clientID = "diagnostic-test-client-id"
	}
	clientSecret := os.Getenv("FUEL_FINDER_CLIENT_SECRET")
	if clientSecret == "" {
		clientSecret = "diagnostic-test-client-secret"
	}

	payloadBytes, _ := json.Marshal(map[string]string{
		"client_id":     clientID,
		"client_secret": clientSecret,
	})

	userAgents := []struct {
		Name  string
		Value string
	}{
		{
			Name:  "Go-Default",
			Value: "Go-http-client/1.1",
		},
		{
			Name:  "Go-v2",
			Value: "Go-http-client/2.0",
		},
		{
			Name:  "cURL-CLI",
			Value: "curl/8.7.1",
		},
		{
			Name:  "Python-Requests",
			Value: "python-requests/2.31.0",
		},
		{
			Name:  "Chrome-Mac",
			Value: "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
		},
		{
			Name:  "Chrome-Windows",
			Value: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/123.0.0.0 Safari/537.36",
		},
		{
			Name:  "Firefox-Linux",
			Value: "Mozilla/5.0 (X11; Linux x86_64; rv:124.0) Gecko/20100101 Firefox/124.0",
		},
		{
			Name:  "Safari-iOS",
			Value: "Mozilla/5.0 (iPhone; CPU iPhone OS 17_4 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Mobile/15E148 Safari/604.1",
		},
		{
			Name:  "Empty-UA",
			Value: "",
		},
		{
			Name:  "Custom-App",
			Value: "PumpCost-FuelTracker/1.0 (+https://stephenford.org/pumpcost)",
		},
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	var results []UserAgentProbeResult
	var count403 int
	var countOriginNginx int

	for _, ua := range userAgents {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(payloadBytes))
		if err != nil {
			t.Fatalf("Failed to construct request: %v", err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		if ua.Value != "" {
			req.Header.Set("User-Agent", ua.Value)
		} else {
			req.Header.Del("User-Agent")
		}

		resp, err := client.Do(req)
		if err != nil {
			t.Logf("[-] Probe [%-16s] Network Error: %v", ua.Name, err)
			continue
		}

		bodyBytes, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		bodySnippet := strings.TrimSpace(string(bodyBytes))
		if len(bodySnippet) > 160 {
			bodySnippet = bodySnippet[:160] + "..."
		}

		server := resp.Header.Get("Server")
		cfPop := resp.Header.Get("X-Amz-Cf-Pop")
		cfID := resp.Header.Get("X-Amz-Cf-Id")
		via := resp.Header.Get("Via")
		cType := resp.Header.Get("Content-Type")

		originReached := strings.Contains(strings.ToLower(server), "nginx")

		if resp.StatusCode == http.StatusForbidden && strings.Contains(server, "CloudFront") {
			count403++
		}
		if originReached {
			countOriginNginx++
		}

		res := UserAgentProbeResult{
			UserAgentName: ua.Name,
			UserAgentVal:  ua.Value,
			StatusCode:    resp.StatusCode,
			Server:        server,
			CloudFrontPop: cfPop,
			CloudFrontID:  cfID,
			Via:           via,
			ContentType:   cType,
			BodySnippet:   bodySnippet,
			OriginReached: originReached,
		}
		results = append(results, res)

		t.Logf("[*] Probe [%-15s] Status: %d | Server: %-15s | POP: %-8s | Body: %s",
			ua.Name, resp.StatusCode, server, cfPop, bodySnippet)

		// Brief pause to avoid rapid bursting
		time.Sleep(200 * time.Millisecond)
	}

	t.Logf("================================================================================")
	t.Logf("DIAGNOSTIC VERDICT:")
	t.Logf("Total Probes:       %d", len(results))
	t.Logf("CloudFront 403s:    %d", count403)
	t.Logf("Origin (Nginx):     %d", countOriginNginx)

	if count403 == len(results) && len(results) > 0 {
		t.Logf("CONCLUSION: ALL user agents (including Chrome, Safari, Firefox, cURL, Go) were rejected with HTTP 403 by CloudFront.")
		t.Logf("--> The blocking mechanism is IP CIDR / ASN / GEO-BLOCKING at the AWS CloudFront/WAF edge.")
		t.Logf("--> User-Agent filtering is NOT the root cause, because legitimate browser User-Agents are also blocked.")
	} else if countOriginNginx == len(results) && len(results) > 0 {
		t.Logf("CONCLUSION: ALL user agents reached the origin Nginx server (Status: %d).", results[0].StatusCode)
		t.Logf("--> Egress IP (%s, %s) is permitted through CloudFront WAF.", ipInfo.IP, ipInfo.Country)
		t.Logf("--> User-Agent filtering is NOT active for these standard agents.")
	} else {
		t.Logf("CONCLUSION: Mixed results observed across User-Agents. Inspect individual probe statuses above.")
	}
	t.Logf("================================================================================")
}
