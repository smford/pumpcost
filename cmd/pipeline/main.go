package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"time"

	"pumpcost/internal/client"
	"pumpcost/internal/sharding"
)

func main() {
	var (
		outputDir        = flag.String("output", getEnv("PUMPCOST_OUTPUT_DIR", getEnv("PUMPCODE_OUTPUT_DIR", "data/outcodes")), "Output directory for sharded JSON files")
		tokenURL         = flag.String("token-url", getEnv("FUEL_FINDER_TOKEN_URL", "https://www.fuel-finder.service.gov.uk/api/v1/oauth/generate_access_token"), "OAuth2 token URL")
		baseURL          = flag.String("base-url", getEnv("FUEL_FINDER_BASE_URL", "https://www.fuel-finder.service.gov.uk"), "Base URL of Fuel Finder API")
		stationsEndpoint = flag.String("stations-endpoint", getEnv("FUEL_FINDER_STATIONS_ENDPOINT", "/api/v1/pfs"), "Endpoint path for stations and prices")
		scope            = flag.String("scope", getEnv("FUEL_FINDER_SCOPE", "fuelfinder.read"), "OAuth2 scope")
		mockMode         = flag.Bool("mock", getEnvBool("PUMPCOST_MOCK", getEnvBool("PUMPCODE_MOCK", false)), "Force mock data generation mode")
		timeoutSec       = flag.Int("timeout", 40, "Timeout in seconds for entire data pipeline")
		serveAddr        = flag.String("serve", "", "Start local HTTP development server on given address (e.g. :8080)")
	)
	flag.Parse()

	startTime := time.Now()
	log.Printf("[PumpCost Pipeline] Starting ingestion and sharding...")

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(*timeoutSec)*time.Second)
	defer cancel()

	clientID := os.Getenv("FUEL_FINDER_CLIENT_ID")
	clientSecret := os.Getenv("FUEL_FINDER_CLIENT_SECRET")

	var stations []sharding.Station
	var err error

	if *mockMode {
		log.Println("[PumpCost Pipeline] Running in mock generation mode.")
		stations = client.GenerateMockStations()
	} else if clientID == "" && clientSecret == "" {
		log.Println("[PumpCost Pipeline] Note: FUEL_FINDER_CLIENT_ID and FUEL_FINDER_CLIENT_SECRET not set. Ingesting live open data feeds.")
		stations = client.FetchOpenDataFeeds(ctx, nil)
	} else {
		log.Printf("[PumpCost Pipeline] Connecting to Fuel Finder API (%s)...", *baseURL)
		c := client.NewClient(client.Config{
			ClientID:         clientID,
			ClientSecret:     clientSecret,
			Scope:            *scope,
			TokenURL:         *tokenURL,
			BaseURL:          *baseURL,
			StationsEndpoint: *stationsEndpoint,
			Timeout:          30 * time.Second,
			MaxRetries:       3,
		})

		stations, err = c.FetchStations(ctx)
		if err != nil {
			log.Printf("[PumpCost Pipeline] WARNING: Upstream API request failed (%v). Ingesting public open data feeds to ensure complete UK coverage.", err)
			stations = client.FetchOpenDataFeeds(ctx, nil)
		}
	}

	log.Printf("[PumpCost Pipeline] Ingested %d forecourt records.", len(stations))

	index, err := sharding.ShardAndWrite(stations, sharding.ShardOptions{
		OutputDir:   *outputDir,
		Timestamp:   time.Now().UTC(),
		Concurrency: 16,
	})
	if err != nil {
		log.Fatalf("[PumpCost Pipeline] ERROR sharding and writing datasets: %v", err)
	}

	elapsed := time.Since(startTime)
	log.Printf("[PumpCost Pipeline] SUCCESS: Generated %d outcodes (%d stations) in %s to %s",
		index.TotalOutcodes, index.TotalStations, elapsed.Round(time.Millisecond), *outputDir)

	if *serveAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/data/", http.StripPrefix("/data/", http.FileServer(http.Dir("data"))))
		mux.Handle("/", http.FileServer(http.Dir("static")))

		addr := *serveAddr
		if addr[0] != ':' {
			addr = ":" + addr
		}
		log.Printf("[PumpCost Server] Serving PumpCost at http://localhost%s (press Ctrl+C to stop)...", addr)
		if err := http.ListenAndServe(addr, mux); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[PumpCost Server] Server error: %v", err)
		}
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvBool(key string, defaultVal bool) bool {
	val := os.Getenv(key)
	if val == "1" || val == "true" || val == "TRUE" {
		return true
	}
	if val == "0" || val == "false" || val == "FALSE" {
		return false
	}
	return defaultVal
}
