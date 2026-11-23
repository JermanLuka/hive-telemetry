package main

import (
	"testing"
	"time"
)

func TestRetryDelay(t *testing.T) {
	tests := []struct {
		attempt int
		want    time.Duration
	}{
		{1, 1 * time.Second},
		{2, 2 * time.Second},
		{3, 4 * time.Second},
		{5, 16 * time.Second},
		{6, 30 * time.Second},
		{1000, 30 * time.Second},
	}
	for _, tt := range tests {
		if got := retryDelay(tt.attempt); got != tt.want {
			t.Errorf("retryDelay(%d) = %v; want %v", tt.attempt, got, tt.want)
		}
	}
}
