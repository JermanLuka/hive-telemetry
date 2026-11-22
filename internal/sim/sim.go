// Package sim generates plausible readings for simulated beehives.
package sim

import (
	"math"
	"math/rand/v2"
	"time"

	"github.com/JermanLuka/hive-telemetry/internal/telemetry"
)

// Hive is one simulated hive. Its weight follows a daily pattern: bees bring
// in nectar during the day and eat from their stores at night.
//
// A Hive is not safe for concurrent use; each one belongs to one goroutine.
type Hive struct {
	ID      string
	weight  float64
	hasTemp bool
	rng     *rand.Rand
}

// NewHive returns a hive that starts at startKg. The seed makes the generated
// values repeatable, which keeps tests deterministic.
func NewHive(id string, startKg float64, hasTemp bool, seed uint64) *Hive {
	return &Hive{
		ID:      id,
		weight:  startKg,
		hasTemp: hasTemp,
		rng:     rand.New(rand.NewPCG(seed, seed)),
	}
}

// Next advances the hive by step and returns the reading observed at t.
func (h *Hive) Next(t time.Time, step time.Duration) telemetry.Reading {
	h.weight += gainPerHour(t.Hour())*step.Hours() + h.rng.NormFloat64()*0.01

	r := telemetry.Reading{
		ObservedAt: t.UTC().Truncate(time.Second), // the API reports whole seconds
		WeightKg:   round(h.weight, 2),            // scales report in 10 g steps
	}
	if h.hasTemp {
		// The brood nest is held near 34.5 °C, with a small daily swing.
		dayFraction := (float64(t.Hour()) + float64(t.Minute())/60) / 24
		temp := 34.5 + 0.3*math.Sin(2*math.Pi*dayFraction) + h.rng.NormFloat64()*0.1
		temp = round(temp, 1)
		r.TemperatureC = &temp
	}
	return r
}

// gainPerHour is the weight change in kg per hour at a given hour of the day.
func gainPerHour(hour int) float64 {
	if hour >= 9 && hour < 17 {
		return 0.10 // foraging: nectar comes in
	}
	return -0.03 // night: the colony eats from its stores
}

func round(x float64, decimals int) float64 {
	p := math.Pow(10, float64(decimals))
	return math.Round(x*p) / p
}
