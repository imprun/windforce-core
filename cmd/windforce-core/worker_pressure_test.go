package main

import (
	"testing"
	"time"
)

func TestNewResourcePressureObserverValidatesEnabledConfiguration(t *testing.T) {
	observer, err := newResourcePressureObserver(false, 0.9, 0.8, 5*time.Second)
	if err != nil || observer == nil {
		t.Fatalf("observer = %#v, err=%v", observer, err)
	}
	if _, err := newResourcePressureObserver(false, 0.8, 0.8, 5*time.Second); err == nil {
		t.Fatal("equal high/low watermarks were accepted")
	}
	if _, err := newResourcePressureObserver(false, 1.1, 0.8, 5*time.Second); err == nil {
		t.Fatal("high watermark above one was accepted")
	}
}

func TestNewResourcePressureObserverDisabledIsCompatibilityEscapeHatch(t *testing.T) {
	observer, err := newResourcePressureObserver(true, 0, 0, 0)
	if err != nil || observer != nil {
		t.Fatalf("disabled observer = %#v, err=%v", observer, err)
	}
}
