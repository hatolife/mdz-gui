package main

import "testing"

func TestDevelopmentVersion(t *testing.T) {
	if developmentVersion != "v0.0.1" {
		t.Fatalf("developmentVersion = %q", developmentVersion)
	}
}
