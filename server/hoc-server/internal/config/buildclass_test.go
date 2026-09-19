package config

import (
	"testing"
	"time"
)

func TestBuildClassAndPeriod(t *testing.T) {
	if !SameBuildClass("3.5.2a", "") || !SameBuildClass("", "") || !SameBuildClass("3.5.2a", "3.5.2a") {
		t.Fatal("stock/unknown builds must share a class")
	}
	if SameBuildClass("3.5.2a", Build60Hz) || SameBuildClass("", Build60Hz) {
		t.Fatal("stock and 60 Hz must not share a class")
	}
	if !SameBuildClass(Build60Hz, Build60Hz) {
		t.Fatal("60 Hz with itself")
	}
	if FramePeriodForBuild("3.5.2a") != FramePeriod || FramePeriodForBuild("") != FramePeriod {
		t.Fatal("stock build must use the default period")
	}
	if FramePeriodForBuild(Build60Hz) != FramePeriod60Hz || FramePeriod60Hz != 16*time.Millisecond {
		t.Fatalf("60 Hz period = %s", FramePeriodForBuild(Build60Hz))
	}
}
