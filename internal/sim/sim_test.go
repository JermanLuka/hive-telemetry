package sim

import (
	"testing"
	"time"
)

var start = time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)

func TestNextIsRepeatableForTheSameSeed(t *testing.T) {
	a := NewHive("a", 40, true, 7)
	b := NewHive("b", 40, true, 7)
	for i := range 50 {
		now := start.Add(time.Duration(i) * time.Minute)
		ra, rb := a.Next(now, time.Minute), b.Next(now, time.Minute)
		if ra.WeightKg != rb.WeightKg || *ra.TemperatureC != *rb.TemperatureC {
			t.Fatalf("step %d: got %+v and %+v; want equal readings", i, ra, rb)
		}
	}
}

func TestNextWithoutTemperatureSensor(t *testing.T) {
	h := NewHive("h", 40, false, 1)
	if r := h.Next(start, time.Minute); r.TemperatureC != nil {
		t.Errorf("TemperatureC = %v; want nil", *r.TemperatureC)
	}
}

func TestNextValuesArePlausible(t *testing.T) {
	h := NewHive("h", 40, true, 1)
	for i := range 24 * 12 {
		now := start.Add(time.Duration(i) * 5 * time.Minute)
		r := h.Next(now, 5*time.Minute)
		if !r.ObservedAt.Equal(now) {
			t.Fatalf("ObservedAt = %v; want %v", r.ObservedAt, now)
		}
		if r.WeightKg < 35 || r.WeightKg > 45 {
			t.Fatalf("WeightKg = %v at %v; want within 5 kg of the start", r.WeightKg, now)
		}
		if temp := *r.TemperatureC; temp < 33 || temp > 36 {
			t.Fatalf("TemperatureC = %v at %v; want about 34.5", temp, now)
		}
	}
}

// Over a day of foraging the hive gains weight; over a night it loses some.
func TestNextFollowsTheDailyPattern(t *testing.T) {
	tests := []struct {
		name      string
		from      time.Time
		wantGrows bool
	}{
		{"day", start.Add(9 * time.Hour), true},
		{"night", start.Add(18 * time.Hour), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := NewHive("h", 40, false, 1)
			first := h.Next(tt.from, 5*time.Minute).WeightKg
			var last float64
			for i := 1; i < 8*12; i++ { // 8 hours in 5-minute steps
				last = h.Next(tt.from.Add(time.Duration(i)*5*time.Minute), 5*time.Minute).WeightKg
			}
			if grows := last > first; grows != tt.wantGrows {
				t.Errorf("weight went from %v to %v; want grows = %v", first, last, tt.wantGrows)
			}
		})
	}
}
