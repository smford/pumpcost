package sharding

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExtractOutcode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"SW1A 1AA", "SW1A"},
		{"sw1a 1aa", "SW1A"},
		{"SW1A1AA", "SW1A"},
		{"SS1 2EF", "SS1"},
		{"ss12ef", "SS1"},
		{"BS1 4ST", "BS1"},
		{"EH1 1YZ", "EH1"},
		{"M1 1AE", "M1"},
		{"B33 8TH", "B33"},
		{"CR2 6XH", "CR2"},
		{"DN55 1PT", "DN55"},
		{"W1A 0AX", "W1A"},
		{"EC1A 1BB", "EC1A"},
		{"G1 1DA", "G1"},
		{"CF10 1EP", "CF10"},
		{"BT1 5GS", "BT1"},
		{"SS1", "SS1"},
		{"invalid_postcode", "UNKNOWN"},
		{"", "UNKNOWN"},
		{"   ", "UNKNOWN"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			actual := ExtractOutcode(tc.input)
			if actual != tc.expected {
				t.Errorf("ExtractOutcode(%q) = %q, expected %q", tc.input, actual, tc.expected)
			}
		})
	}
}

func TestShardAndWrite(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "pumpcost-sharding-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.UTC)
	stations := []Station{
		{
			ID:       "st-1",
			Brand:    "Tesco",
			Name:     "Tesco Southend",
			Postcode: "SS1 2EF",
			Lat:      51.54,
			Lon:      0.71,
			Prices: map[string]float64{
				"E10": 138.9,
				"B7":  143.9,
			},
			Amenities: []string{"car_wash", "air_water"},
		},
		{
			ID:       "st-2",
			Brand:    "Shell",
			Name:     "Shell Victoria",
			Postcode: "SW1A 1AA",
			Lat:      51.50,
			Lon:      -0.14,
			Prices: map[string]float64{
				"E10": 142.9,
				"B7":  148.9,
			},
			Amenities: []string{"coffee"},
		},
		{
			ID:       "st-3",
			Brand:    "BP",
			Name:     "BP Southend East",
			Postcode: "SS1 3XX",
			Lat:      51.53,
			Lon:      0.72,
			Prices: map[string]float64{
				"E10": 139.9,
				"B7":  144.9,
			},
			Amenities: []string{"air_water"},
		},
	}

	index, err := ShardAndWrite(stations, ShardOptions{
		OutputDir: tempDir,
		Timestamp: now,
	})
	if err != nil {
		t.Fatalf("ShardAndWrite failed: %v", err)
	}

	if index.TotalStations != 3 {
		t.Errorf("expected 3 total stations, got %d", index.TotalStations)
	}
	if index.TotalOutcodes != 2 {
		t.Errorf("expected 2 outcodes (SS1 and SW1A), got %d", index.TotalOutcodes)
	}

	// Verify SS1 file
	ss1Path := filepath.Join(tempDir, "SS1.json")
	data, err := os.ReadFile(ss1Path)
	if err != nil {
		t.Fatalf("failed to read SS1.json: %v", err)
	}

	var ss1Payload OutcodePayload
	if err := json.Unmarshal(data, &ss1Payload); err != nil {
		t.Fatalf("failed to unmarshal SS1.json: %v", err)
	}

	if ss1Payload.Outcode != "SS1" {
		t.Errorf("expected outcode SS1, got %q", ss1Payload.Outcode)
	}
	if len(ss1Payload.Stations) != 2 {
		t.Errorf("expected 2 stations in SS1, got %d", len(ss1Payload.Stations))
	}

	// Verify index.json file
	indexPath := filepath.Join(tempDir, "index.json")
	indexBytes, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("failed to read index.json: %v", err)
	}

	var diskIndex IndexPayload
	if err := json.Unmarshal(indexBytes, &diskIndex); err != nil {
		t.Fatalf("failed to unmarshal index.json: %v", err)
	}

	if diskIndex.TotalStations != 3 {
		t.Errorf("expected 3 total stations in index.json, got %d", diskIndex.TotalStations)
	}
}
