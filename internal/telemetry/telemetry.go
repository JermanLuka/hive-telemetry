// Package telemetry defines the MQTT messages that hives publish: the topic
// each hive writes to and the JSON payload of a reading.
package telemetry

import (
	"strings"
	"time"
)

// QoS is the MQTT quality of service for readings. Level 1 means "at least
// once": the broker retries until the message is acknowledged, so a reading
// can arrive more than once and the consumer has to ignore duplicates.
const QoS byte = 1

// ReadingTopic returns the topic a hive publishes its readings to. The hive
// ID is part of the topic, not the payload, so the broker can route and
// authorise per hive.
func ReadingTopic(hiveID string) string {
	return "hives/" + hiveID + "/readings"
}

// ReadingSubscription matches the reading topic of every hive; "+" is the
// MQTT wildcard for exactly one topic level.
const ReadingSubscription = "hives/+/readings"

// HiveIDFromTopic returns the hive ID in a reading topic. It reports false
// if the topic is not of the form hives/{hiveID}/readings.
func HiveIDFromTopic(topic string) (string, bool) {
	rest, ok := strings.CutPrefix(topic, "hives/")
	if !ok {
		return "", false
	}
	id, ok := strings.CutSuffix(rest, "/readings")
	if !ok || id == "" || strings.Contains(id, "/") {
		return "", false
	}
	return id, true
}

// Reading is one measurement from a hive's scale and sensors.
type Reading struct {
	// ObservedAt is when the hive took the measurement, not when it was sent.
	ObservedAt time.Time `json:"observed_at"`
	WeightKg   float64   `json:"weight_kg"`
	// TemperatureC is nil for hives without a temperature sensor.
	TemperatureC *float64 `json:"temperature_c,omitempty"`
}
