package search

import (
	"testing"

	"jobradar/internal/job"
)

func TestParseFreehire(t *testing.T) {
	row := map[string]any{
		"public_slug": "senior-sre-acme",
		"url":         "https://acme.example/jobs/42",
		"title":       "Senior Site Reliability Engineer",
		"company":     "Acme",
		"location":    "Remote",
		"posted_at":   "2026-11-10",
		"work_mode":   "remote",
		"description": "<p>Kubernetes and Go</p>",
	}

	posting, ok := parse(Freehire, row)
	if !ok {
		t.Fatal("row was dropped")
	}
	if posting.Key != "freehire:senior-sre-acme" {
		t.Errorf("key = %q", posting.Key)
	}
	if !posting.Remote {
		t.Error("remote flag not read from work_mode")
	}
	if posting.Preview != "Kubernetes and Go" {
		t.Errorf("preview = %q, want the tags stripped", posting.Preview)
	}
}

func TestParseLinkedIn(t *testing.T) {
	row := map[string]any{
		"id":          "4489639283",
		"url":         "https://www.linkedin.com/jobs/view/4489639283",
		"title":       "Platform Engineer",
		"company":     "Canonical",
		"location":    "Lagos, Nigeria",
		"posted_date": "2026-11-10",
		"remote":      true,
	}

	posting, ok := parse(LinkedIn, row)
	if !ok {
		t.Fatal("row was dropped")
	}
	if posting.Key != "linkedin:4489639283" {
		t.Errorf("key = %q", posting.Key)
	}
	if posting.Posted != "2026-11-10" {
		t.Errorf("posted = %q", posting.Posted)
	}
}

func TestParseDropsIncompleteRows(t *testing.T) {
	for name, row := range map[string]map[string]any{
		"no id":      {"url": "https://x.example/1", "title": "T", "company": "C"},
		"no url":     {"public_slug": "a", "title": "T", "company": "C"},
		"no title":   {"public_slug": "a", "url": "https://x.example/1", "company": "C"},
		"no company": {"public_slug": "a", "url": "https://x.example/1", "title": "T"},
	} {
		if _, ok := parse(Freehire, row); ok {
			t.Errorf("%s: row was kept", name)
		}
	}
}

func TestParseUnknownSource(t *testing.T) {
	if _, ok := parse("indeed", map[string]any{"id": "1"}); ok {
		t.Error("unknown source parsed")
	}
}

func TestURLKeyIgnoresTrackingAndCase(t *testing.T) {
	base := job.URLKey("https://Jobs.Example.com/role/42")
	for _, other := range []string{
		"https://jobs.example.com/role/42/",
		"https://Jobs.Example.com/role/42?utm_source=linkedin",
		"https://jobs.example.com/role/42#apply",
	} {
		if got := job.URLKey(other); got != base {
			t.Errorf("URLKey(%q) differs from the bare URL", other)
		}
	}
	if job.URLKey("https://jobs.example.com/role/43") == base {
		t.Error("different postings share a URL key")
	}
}
