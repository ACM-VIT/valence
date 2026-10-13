package main

import (
	"os"
	"strings"
	"testing"
)

func TestLoadAllRejectsRepeatedSlugs(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, dir := range []string{a, b} {
		if err := os.CopyFS(dir, os.DirFS("../../../../problems/examples/aplusb")); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := loadAll([]string{a}); err != nil {
		t.Fatalf("one package: %v", err)
	}
	_, err := loadAll([]string{a, b})
	if err == nil || !strings.Contains(err.Error(), "1 of 2 packages are invalid") {
		t.Fatalf("two packages with the same slug: got %v, want 1 of 2 invalid", err)
	}
}
