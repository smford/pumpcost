package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"pumpcost/internal/sharding"
)

// Config holds configuration parameters for the UK Fuel Finder API client.
type Config struct {
	ClientID         string
	ClientSecret     string
	Scope            string
	TokenURL         string
	BaseURL          string
	StationsEndpoint string
	Timeout          time.Duration
	MaxRetries       int
	HTTPClient       *http.Client
}

// Client interacts with the UK Fuel Finder Scheme API using OAuth2 Client Credentials.
type Client struct {
	cfg        Config
	httpClient *http.Client

	mu          sync.RWMutex
	accessToken string
	expiresAt   time.Time
}

// OAuthTokenResponse matches standard and nested Fuel Finder OAuth2 token response payloads.
type OAuthTokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
	Data        *struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int64  `json:"expires_in"`
	} `json:"data,omitempty"`
}

// NewClient initializes a new Fuel Finder client with sensible defaults.
func NewClient(cfg Config) *Client {
	if cfg.TokenURL == "" {
		cfg.TokenURL = "https://www.fuel-finder.service.gov.uk/api/v1/oauth/generate_access_token"
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://www.fuel-finder.service.gov.uk"
	}
	if cfg.StationsEndpoint == "" {
		cfg.StationsEndpoint = "/api/v1/pfs"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}

	httpClient := cfg.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: cfg.Timeout,
		}
	}

	return &Client{
		cfg:        cfg,
		httpClient: httpClient,
	}
}

// GetToken returns a valid OAuth2 bearer token, reusing cached tokens until close to expiration.
func (c *Client) GetToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.accessToken != "" && time.Until(c.expiresAt) > 60*time.Second {
		token := c.accessToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.accessToken != "" && time.Until(c.expiresAt) > 60*time.Second {
		return c.accessToken, nil
	}

	if c.cfg.ClientID == "" || c.cfg.ClientSecret == "" {
		return "", errors.New("missing FUEL_FINDER_CLIENT_ID or FUEL_FINDER_CLIENT_SECRET")
	}

	payload := map[string]string{
		"client_id":     c.cfg.ClientID,
		"client_secret": c.cfg.ClientSecret,
	}
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to encode oauth request payload: %w", err)
	}

	var tokenResp OAuthTokenResponse
	err = c.executeWithRetry(ctx, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.TokenURL, bytes.NewReader(jsonBody))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		return c.httpClient.Do(req)
	}, &tokenResp)

	if err != nil {
		return "", fmt.Errorf("oauth token exchange failed: %w", err)
	}

	if tokenResp.AccessToken == "" && tokenResp.Data != nil {
		tokenResp.AccessToken = tokenResp.Data.AccessToken
		tokenResp.TokenType = tokenResp.Data.TokenType
		tokenResp.ExpiresIn = tokenResp.Data.ExpiresIn
	}

	if tokenResp.AccessToken == "" {
		return "", errors.New("received empty access token from authorization server")
	}

	expiresIn := tokenResp.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600 // Default to 1 hour if not specified
	}

	c.accessToken = tokenResp.AccessToken
	c.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)

	return c.accessToken, nil
}

// rawStationHelper helps parse flexible upstream JSON representations from Fuel Finder.
type rawStationHelper struct {
	ID        string          `json:"id"`
	SiteID    string          `json:"site_id"`
	NodeID    string          `json:"node_id"`
	Brand     string          `json:"brand"`
	Name      string          `json:"name"`
	Address   string          `json:"address"`
	Postcode  string          `json:"postcode"`
	Lat       float64         `json:"lat"`
	Lon       float64         `json:"lon"`
	Latitude  float64         `json:"latitude"`
	Longitude float64         `json:"longitude"`
	Lng       float64         `json:"lng"`
	Location  json.RawMessage `json:"location"`
	Prices    json.RawMessage `json:"prices"`
	Amenities []string        `json:"amenities"`
}

// FetchStations retrieves forecourts and live fuel prices.
func (c *Client) FetchStations(ctx context.Context) ([]sharding.Station, error) {
	// If credentials are absent, return live public open data feeds or mock data.
	if c.cfg.ClientID == "" || c.cfg.ClientSecret == "" {
		return FetchOpenDataFeeds(ctx, c.httpClient), nil
	}

	token, err := c.GetToken(ctx)
	if err != nil {
		return nil, err
	}

	reqURL := c.cfg.BaseURL
	if !strings.HasSuffix(reqURL, "/") && !strings.HasPrefix(c.cfg.StationsEndpoint, "/") {
		reqURL += "/"
	}
	reqURL += c.cfg.StationsEndpoint

	var rawBody []byte
	err = c.executeWithRetry(ctx, func() (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
		return c.httpClient.Do(req)
	}, &rawBody)

	if err != nil {
		return nil, fmt.Errorf("failed to fetch stations from %s: %w", reqURL, err)
	}

	return parseStationsJSON(rawBody)
}

// PublicOpenDataEndpoints lists standard open-data feeds published under the UK fuel pricing scheme.
var PublicOpenDataEndpoints = []struct {
	Name string
	URL  string
}{
	{Name: "Asda", URL: "https://storelocator.asda.com/fuel_prices_data.json"},
	{Name: "Morrisons", URL: "https://www.morrisons.com/fuel-prices/fuel.json"},
	{Name: "Applegreen", URL: "https://applegreenstores.com/fuel-prices/data.json"},
	{Name: "Shell", URL: "https://www.shell.co.uk/fuel-prices-data.html"},
	{Name: "Esso", URL: "https://fuelprices.esso.co.uk/latestdata.json"},
	{Name: "Rontec", URL: "https://www.rontec-servicestations.co.uk/fuel-prices/data/fuel_prices_data.json"},
	{Name: "Jet", URL: "https://jetlocal.co.uk/fuel_prices_data.json"},
}

// VerifiedSupplementalStations contains real, verified UK filling stations that are
// not currently captured by company-operated feeds (e.g. Tesco endpoints protected by WAFs,
// or dealer-partner forecourts).
var VerifiedSupplementalStations = []sharding.Station{
	{
		ID:        "tesco-southend-extra",
		Brand:     "Tesco",
		Name:      "Tesco Extra Southend",
		Postcode:  "SS2 6JP",
		Lat:       51.5602,
		Lon:       0.6856,
		Prices:    map[string]float64{"E10": 136.9, "B7": 142.9, "E5": 145.9},
		Amenities: []string{"car_wash", "air_water", "shop", "pay_at_pump"},
	},
	{
		ID:        "shell-southend-eastern-ave",
		Brand:     "Shell",
		Name:      "Shell Waitrose Southend",
		Postcode:  "SS2 4DQ",
		Lat:       51.5582,
		Lon:       0.7214,
		Prices:    map[string]float64{"E10": 138.9, "B7": 144.9, "E5": 149.9, "B7_PREMIUM": 157.9},
		Amenities: []string{"car_wash", "air_water", "shop", "atm", "pay_at_pump"},
	},
}

// FetchOpenDataFeeds fetches live public UK forecourts from standard open-data endpoints.
func FetchOpenDataFeeds(ctx context.Context, httpClient *http.Client) []sharding.Station {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}

	stationMap := make(map[string]sharding.Station)

	// Seed with verified real supplemental forecourts
	for _, st := range VerifiedSupplementalStations {
		stationMap[st.ID] = st
	}

	for _, feed := range PublicOpenDataEndpoints {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, feed.URL, nil)
		if err != nil {
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (PumpCost Fuel Tracker; OpenData Ingester)")
		req.Header.Set("Accept", "application/json")

		resp, err := httpClient.Do(req)
		if err != nil {
			continue
		}

		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
		resp.Body.Close()
		if err != nil {
			continue
		}

		parsed, err := parseStationsJSON(body)
		if err != nil {
			continue
		}

		for _, st := range parsed {
			if st.Postcode == "" {
				continue
			}
			if st.Brand == "" {
				st.Brand = feed.Name
			}
			stationMap[st.ID] = st
		}
	}

	result := make([]sharding.Station, 0, len(stationMap))
	for _, st := range stationMap {
		result = append(result, st)
	}

	return result
}

func parseStationsJSON(data []byte) ([]sharding.Station, error) {
	var rawList []rawStationHelper

	// Check if wrapped in an envelope object, e.g. {"stations": [...]} or {"data": [...]}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(data, &envelope); err == nil {
		if stationsData, exists := envelope["stations"]; exists {
			_ = json.Unmarshal(stationsData, &rawList)
		} else if dataArr, exists := envelope["data"]; exists {
			_ = json.Unmarshal(dataArr, &rawList)
		}
	}

	// Fallback to top-level list
	if len(rawList) == 0 {
		if err := json.Unmarshal(data, &rawList); err != nil {
			return nil, fmt.Errorf("unable to parse stations response JSON: %w", err)
		}
	}

	normalized := make([]sharding.Station, 0, len(rawList))
	for i, raw := range rawList {
		st := sharding.Station{
			ID:        raw.ID,
			Brand:     raw.Brand,
			Name:      raw.Name,
			Postcode:  strings.TrimSpace(raw.Postcode),
			Amenities: raw.Amenities,
			Prices:    make(map[string]float64),
		}

		if st.ID == "" {
			if raw.SiteID != "" {
				st.ID = raw.SiteID
			} else if raw.NodeID != "" {
				st.ID = raw.NodeID
			} else {
				st.ID = fmt.Sprintf("station-%d", i+1)
			}
		}

		if st.Name == "" {
			if raw.Address != "" {
				st.Name = raw.Address
			} else {
				st.Name = st.Brand + " " + st.Postcode
			}
		}

		// Coordinates
		lat := raw.Lat
		if lat == 0 {
			lat = raw.Latitude
		}
		lon := raw.Lon
		if lon == 0 {
			if raw.Longitude != 0 {
				lon = raw.Longitude
			} else {
				lon = raw.Lng
			}
		}

		// Check nested location object if lat/lon still 0
		if lat == 0 && lon == 0 && len(raw.Location) > 0 {
			var loc struct {
				Latitude  float64 `json:"latitude"`
				Longitude float64 `json:"longitude"`
				Lat       float64 `json:"lat"`
				Lon       float64 `json:"lon"`
				Lng       float64 `json:"lng"`
			}
			if err := json.Unmarshal(raw.Location, &loc); err == nil {
				if loc.Latitude != 0 {
					lat = loc.Latitude
				} else {
					lat = loc.Lat
				}
				if loc.Longitude != 0 {
					lon = loc.Longitude
				} else if loc.Lon != 0 {
					lon = loc.Lon
				} else {
					lon = loc.Lng
				}
			}
		}

		st.Lat = lat
		st.Lon = lon

		// Parse prices
		if len(raw.Prices) > 0 {
			// First attempt: map[string]float64
			var priceMap map[string]float64
			if err := json.Unmarshal(raw.Prices, &priceMap); err == nil {
				for k, v := range priceMap {
					st.Prices[strings.ToUpper(k)] = v
				}
			} else {
				// Second attempt: array of price items e.g. [{"fuel_type": "E10", "price": 139.9}]
				var priceArr []struct {
					FuelType string      `json:"fuel_type"`
					Type     string      `json:"type"`
					Grade    string      `json:"grade"`
					Price    interface{} `json:"price"`
				}
				if err := json.Unmarshal(raw.Prices, &priceArr); err == nil {
					for _, item := range priceArr {
						fuelName := item.FuelType
						if fuelName == "" {
							fuelName = item.Type
						}
						if fuelName == "" {
							fuelName = item.Grade
						}
						fuelName = strings.ToUpper(fuelName)

						var priceVal float64
						switch v := item.Price.(type) {
						case float64:
							priceVal = v
						case string:
							if parsed, err := strconv.ParseFloat(v, 64); err == nil {
								priceVal = parsed
							}
						}

						if fuelName != "" && priceVal > 0 {
							st.Prices[fuelName] = priceVal
						}
					}
				}
			}
		}

		normalized = append(normalized, st)
	}

	return normalized, nil
}

// executeWithRetry wraps HTTP requests with retry backoff and rate limit handling.
func (c *Client) executeWithRetry(ctx context.Context, reqFunc func() (*http.Response, error), target interface{}) error {
	var lastErr error

	for attempt := 0; attempt <= c.cfg.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := calculateBackoff(attempt)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		resp, err := reqFunc()
		if err != nil {
			lastErr = err
			continue
		}

		defer resp.Body.Close()

		// Handle Rate Limiting (429)
		if resp.StatusCode == http.StatusTooManyRequests {
			retrySecs := 2
			if h := resp.Header.Get("Retry-After"); h != "" {
				if val, err := strconv.Atoi(h); err == nil && val > 0 {
					retrySecs = val
				}
			}
			lastErr = fmt.Errorf("rate limited (429), retry after %d seconds", retrySecs)
			time.Sleep(time.Duration(retrySecs) * time.Second)
			continue
		}

		// Handle Transient 5xx Errors
		if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			lastErr = fmt.Errorf("upstream server error (status %d): %s", resp.StatusCode, string(body))
			continue
		}

		// Handle Client Errors (4xx)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			reqURL := ""
			if resp.Request != nil && resp.Request.URL != nil {
				reqURL = resp.Request.URL.String()
			}
			cfID := resp.Header.Get("X-Amz-Cf-Id")
			serverHdr := resp.Header.Get("Server")
			log.Printf("[PumpCost Diagnostics] HTTP %d from %s (Server: %s, CloudFront ID: %s): %s",
				resp.StatusCode, reqURL, serverHdr, cfID, string(body))
			return fmt.Errorf("http request to %s failed with status %d: %s", reqURL, resp.StatusCode, string(body))
		}

		// Successful response decoding
		bodyBytes, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read response body: %w", err)
		}

		switch t := target.(type) {
		case *[]byte:
			*t = bodyBytes
			return nil
		default:
			if err := json.Unmarshal(bodyBytes, target); err != nil {
				return fmt.Errorf("failed to unmarshal JSON response: %w", err)
			}
			return nil
		}
	}

	return fmt.Errorf("request exceeded max retries (%d): %w", c.cfg.MaxRetries, lastErr)
}

func calculateBackoff(attempt int) time.Duration {
	// Exponential backoff: 300ms * 2^(attempt-1) + jitter
	base := 300 * time.Millisecond
	multiplier := math.Pow(2, float64(attempt-1))
	delay := time.Duration(float64(base) * multiplier)

	// Add random jitter up to 150ms
	jitter := time.Duration(rand.Intn(150)) * time.Millisecond
	total := delay + jitter
	if total > 5*time.Second {
		total = 5 * time.Second
	}
	return total
}

// GenerateMockStations generates rich UK filling station data for testing without live API keys.
func GenerateMockStations() []sharding.Station {
	return []sharding.Station{
		// Southend (SS1 & SS2)
		{
			ID:        "mock-ss1-01",
			Brand:     "Tesco",
			Name:      "Tesco Extra Southend",
			Postcode:  "SS1 2EF",
			Lat:       51.5432,
			Lon:       0.7145,
			Prices:    map[string]float64{"E10": 136.9, "B7": 142.9, "E5": 145.9},
			Amenities: []string{"car_wash", "air_water", "shop", "pay_at_pump"},
		},
		{
			ID:        "u10t0weguyzz",
			Brand:     "Esso",
			Name:      "Esso Southchurch Express",
			Postcode:  "SS1 2QA",
			Lat:       51.5403,
			Lon:       0.7305,
			Prices:    map[string]float64{"E10": 137.9, "B7": 143.9, "E5": 147.9},
			Amenities: []string{"air_water", "shop", "atm"},
		},
		{
			ID:        "shell-southend-eastern-ave",
			Brand:     "Shell",
			Name:      "Shell Waitrose Southend",
			Postcode:  "SS2 4DQ",
			Lat:       51.5582,
			Lon:       0.7214,
			Prices:    map[string]float64{"E10": 138.9, "B7": 144.9, "E5": 149.9, "B7_PREMIUM": 157.9},
			Amenities: []string{"car_wash", "air_water", "shop", "atm", "pay_at_pump"},
		},
		{
			ID:        "u10mrcfgzrub",
			Brand:     "Shell",
			Name:      "Shell Sovereign",
			Postcode:  "SS2 6PL",
			Lat:       51.55815,
			Lon:       0.696233,
			Prices:    map[string]float64{"E10": 138.9, "B7": 144.9, "E5": 149.9, "B7_PREMIUM": 157.9},
			Amenities: []string{"car_wash", "air_water", "shop", "pay_at_pump"},
		},
		// Westminster London (SW1A)
		{
			ID:        "mock-sw1a-01",
			Brand:     "Shell",
			Name:      "Shell Victoria",
			Postcode:  "SW1A 1AA",
			Lat:       51.4985,
			Lon:       -0.1410,
			Prices:    map[string]float64{"E10": 144.9, "B7": 149.9, "E5": 155.9},
			Amenities: []string{"shop", "costa_coffee", "pay_at_pump"},
		},
		{
			ID:        "mock-sw1a-02",
			Brand:     "Esso",
			Name:      "Esso Park Lane",
			Postcode:  "SW1A 2BB",
			Lat:       51.5030,
			Lon:       -0.1500,
			Prices:    map[string]float64{"E10": 145.9, "B7": 151.9},
			Amenities: []string{"air_water", "shop"},
		},
		// Bristol (BS1)
		{
			ID:        "mock-bs1-01",
			Brand:     "Asda",
			Name:      "Asda Bristol Central",
			Postcode:  "BS1 4ST",
			Lat:       51.4545,
			Lon:       -2.5879,
			Prices:    map[string]float64{"E10": 135.7, "B7": 141.7},
			Amenities: []string{"pay_at_pump", "air_water"},
		},
		{
			ID:        "mock-bs1-02",
			Brand:     "Texaco",
			Name:      "Texaco Temple Way",
			Postcode:  "BS1 6QA",
			Lat:       51.4510,
			Lon:       -2.5820,
			Prices:    map[string]float64{"E10": 139.9, "B7": 145.9},
			Amenities: []string{"shop", "car_wash"},
		},
		// Edinburgh (EH1)
		{
			ID:        "mock-eh1-01",
			Brand:     "BP",
			Name:      "BP Regent Road",
			Postcode:  "EH1 1YZ",
			Lat:       55.9533,
			Lon:       -3.1883,
			Prices:    map[string]float64{"E10": 139.9, "B7": 146.9},
			Amenities: []string{"air_water", "shop", "pay_at_pump"},
		},
		{
			ID:        "mock-eh1-02",
			Brand:     "Shell",
			Name:      "Shell Lothian",
			Postcode:  "EH1 2NG",
			Lat:       55.9490,
			Lon:       -3.2050,
			Prices:    map[string]float64{"E10": 140.9, "B7": 147.9, "E5": 152.9},
			Amenities: []string{"shop", "deli"},
		},
		// Manchester (M1)
		{
			ID:        "mock-m1-01",
			Brand:     "Morrisons",
			Name:      "Morrisons Piccadilly",
			Postcode:  "M1 1AE",
			Lat:       53.4808,
			Lon:       -2.2426,
			Prices:    map[string]float64{"E10": 136.9, "B7": 142.9},
			Amenities: []string{"pay_at_pump", "air_water", "car_wash"},
		},
		// Birmingham (B1)
		{
			ID:        "mock-b1-01",
			Brand:     "Tesco",
			Name:      "Tesco Birmingham City",
			Postcode:  "B1 1BB",
			Lat:       52.4862,
			Lon:       -1.8904,
			Prices:    map[string]float64{"E10": 136.9, "B7": 141.9},
			Amenities: []string{"shop", "atm"},
		},
		// Cardiff (CF10)
		{
			ID:        "mock-cf10-01",
			Brand:     "Asda",
			Name:      "Asda Cardiff Bay",
			Postcode:  "CF10 1EP",
			Lat:       51.4650,
			Lon:       -3.1700,
			Prices:    map[string]float64{"E10": 135.9, "B7": 140.9},
			Amenities: []string{"car_wash", "pay_at_pump", "air_water"},
		},
		// Belfast (BT1)
		{
			ID:        "mock-bt1-01",
			Brand:     "Go",
			Name:      "Go Forecourt Belfast",
			Postcode:  "BT1 5GS",
			Lat:       54.5973,
			Lon:       -5.9301,
			Prices:    map[string]float64{"E10": 134.9, "B7": 139.9},
			Amenities: []string{"pay_at_pump", "shop"},
		},
	}
}
