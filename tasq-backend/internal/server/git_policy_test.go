package server

import "testing"

func TestNormalizeProjectGitPolicy(t *testing.T) {
	if got := normalizeProjectGitPolicy(""); got != "optional" {
		t.Fatalf("empty policy = %q, want optional", got)
	}
	if got := normalizeProjectGitPolicy("required"); got != "required" {
		t.Fatalf("required policy = %q, want required", got)
	}
	if got := normalizeProjectGitPolicy("invalid"); got != "optional" {
		t.Fatalf("invalid project policy = %q, want optional", got)
	}
}

func TestNormalizeTaskGitPolicy(t *testing.T) {
	if got := normalizeTaskGitPolicy(""); got != "inherit" {
		t.Fatalf("empty task policy = %q, want inherit", got)
	}
	if got := normalizeTaskGitPolicy("required"); got != "required" {
		t.Fatalf("required task policy = %q, want required", got)
	}
	if got := normalizeTaskGitPolicy("not_required"); got != "not_required" {
		t.Fatalf("not_required task policy = %q, want not_required", got)
	}
	if got := normalizeTaskGitPolicy("invalid"); got != "inherit" {
		t.Fatalf("invalid task policy = %q, want inherit", got)
	}
}
