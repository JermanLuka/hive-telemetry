package telemetry

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHiveIDFromTopic(t *testing.T) {
	tests := []struct {
		topic  string
		wantID string
		wantOK bool
	}{
		{"hives/hive-1/readings", "hive-1", true},
		{"hives/a/readings", "a", true},
		{"hives//readings", "", false},
		{"hives/a/b/readings", "", false},
		{"hives/hive-1/status", "", false},
		{"other/hive-1/readings", "", false},
		{"hives/hive-1/readings/extra", "", false},
		{"", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.topic, func(t *testing.T) {
			id, ok := HiveIDFromTopic(tt.topic)
			if id != tt.wantID || ok != tt.wantOK {
				t.Errorf("HiveIDFromTopic(%q) = %q, %v; want %q, %v", tt.topic, id, ok, tt.wantID, tt.wantOK)
			}
		})
	}
}

func TestReadingTopicRoundTrip(t *testing.T) {
	id, ok := HiveIDFromTopic(ReadingTopic("hive-7"))
	if !ok || id != "hive-7" {
		t.Errorf("round trip gave %q, %v; want hive-7, true", id, ok)
	}
}

func TestReadingJSONOmitsMissingTemperature(t *testing.T) {
	r := Reading{ObservedAt: time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC), WeightKg: 42.5}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "temperature_c") {
		t.Errorf("payload %s contains temperature_c; want it omitted", b)
	}
}
