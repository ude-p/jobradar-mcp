// These tests drive the search engine through its two interfaces with fakes, so
// they cover the target, priority, pagination and limits without a provider.
package unit

import (
	"context"
	"errors"
	"testing"

	"jobradar/internal/job"
	"jobradar/internal/search"
)

type call struct {
	source string
	text   string
	offset int
}

type fakeProvider struct {
	pages map[call][]job.Job
	errs  map[call]error
	calls []call
}

func (f *fakeProvider) Search(_ context.Context, source string, request search.Request, offset int) ([]job.Job, error) {
	key := call{source: source, text: request.Query, offset: offset}
	f.calls = append(f.calls, key)
	if err, ok := f.errs[key]; ok {
		return nil, err
	}
	return f.pages[key], nil
}

type fakeMemory struct {
	blocked map[string]bool
}

func newMemory() *fakeMemory {
	return &fakeMemory{blocked: map[string]bool{}}
}

func (f *fakeMemory) Blocked(_ context.Context, key, urlKey string) (bool, error) {
	return f.blocked[key] || f.blocked[urlKey], nil
}

func (f *fakeMemory) Touch(_ context.Context, _ []job.Job) error {
	return nil
}

// postings builds count jobs. A shared urlID makes two postings the same URL.
func postings(source string, first, count int) []job.Job {
	items := make([]job.Job, 0, count)
	for index := range count {
		id := first + index
		items = append(items, job.Job{
			Key:     source + ":" + itoa(id),
			Source:  source,
			ID:      itoa(id),
			URL:     "https://example.com/" + source + "/" + itoa(id),
			Title:   "Platform Engineer",
			Company: "Acme",
		})
	}
	return items
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := ""
	for value > 0 {
		digits = string(rune('0'+value%10)) + digits
		value /= 10
	}
	return digits
}

func run(t *testing.T, provider *fakeProvider, memory *fakeMemory, query search.Query) search.Result {
	t.Helper()
	result, err := search.New(memory, provider).Search(context.Background(), query)
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	return result
}

// The target is global: one stream does not cap the answer at its own page.
func TestGlobalTargetAcrossStreams(t *testing.T) {
	provider := &fakeProvider{pages: map[call][]job.Job{
		{source: search.Freehire, text: "platform engineer", offset: 0}: postings(search.Freehire, 0, 2),
		{source: search.LinkedIn, text: "platform engineer", offset: 0}: postings(search.LinkedIn, 0, 3),
	}}

	result := run(t, provider, newMemory(), search.Query{Texts: []string{"platform engineer"}, Count: 4})

	if len(result.Jobs) != 4 {
		t.Fatalf("returned %d jobs, want 4", len(result.Jobs))
	}
	if len(provider.calls) != 2 {
		t.Fatalf("calls = %v, want one page per stream", provider.calls)
	}
	if provider.calls[0].source != search.Freehire || provider.calls[1].source != search.LinkedIn {
		t.Errorf("priority order = %v, want freehire then linkedin", provider.calls)
	}
}

// Sources come first, then the caller's query order.
func TestStreamPriority(t *testing.T) {
	provider := &fakeProvider{pages: map[call][]job.Job{
		{source: search.Freehire, text: "platform engineer", offset: 0}: postings(search.Freehire, 0, 1),
		{source: search.Freehire, text: "devops engineer", offset: 0}:   postings(search.Freehire, 10, 1),
	}}

	result := run(t, provider, newMemory(), search.Query{
		Texts: []string{"platform engineer", "devops engineer"},
		Count: 2,
	})

	if len(result.Jobs) != 2 {
		t.Fatalf("returned %d jobs, want 2", len(result.Jobs))
	}
	if provider.calls[0].text != "platform engineer" || provider.calls[1].text != "devops engineer" {
		t.Errorf("query order = %v, want the caller's order", provider.calls)
	}
}

// The same URL twice in one run counts once and does not fill the target.
func TestDuplicateWithinRun(t *testing.T) {
	first := postings(search.Freehire, 0, 1)[0]
	duplicate := first
	duplicate.Key = "freehire:other"
	duplicate.ID = "other"

	provider := &fakeProvider{pages: map[call][]job.Job{
		{source: search.Freehire, text: "sre", offset: 0}: {first, duplicate},
	}}

	// Count 2, so both rows are examined; a count of 1 would stop after the first.
	result := run(t, provider, newMemory(), search.Query{Texts: []string{"sre"}, Count: 2})

	if len(result.Jobs) != 1 {
		t.Fatalf("returned %d jobs, want 1", len(result.Jobs))
	}
	if result.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", result.Skipped)
	}
}

// A decided posting is skipped and does not consume the target, so the stream
// keeps paging for eligible ones.
func TestDecidedJobsDoNotCount(t *testing.T) {
	memory := newMemory()
	page := postings(search.Freehire, 0, 10)
	for index := range 9 {
		memory.blocked[page[index].Key] = true
	}

	provider := &fakeProvider{pages: map[call][]job.Job{
		{source: search.Freehire, text: "sre", offset: 0}:  page,
		{source: search.Freehire, text: "sre", offset: 10}: postings(search.Freehire, 10, 10),
	}}

	result := run(t, provider, memory, search.Query{Texts: []string{"sre"}, Count: 3})

	if len(result.Jobs) != 3 {
		t.Fatalf("returned %d jobs, want 3", len(result.Jobs))
	}
	if result.Skipped != 9 {
		t.Errorf("skipped = %d, want 9", result.Skipped)
	}
	// Breadth fetches linkedin's first page before freehire's second, so look
	// for the offset rather than for a position.
	second := false
	for _, item := range provider.calls {
		if item.source == search.Freehire && item.offset == 10 {
			second = true
		}
	}
	if !second {
		t.Errorf("calls = %v, want a second freehire page at offset 10", provider.calls)
	}
}

// A row that points at an aggregator is dropped before it can fill the target.
func TestAggregatorURLsDropped(t *testing.T) {
	firstParty := postings(search.Freehire, 0, 1)[0]
	republished := postings(search.Freehire, 5, 1)[0]
	republished.URL = "https://pt.whatjobs.com/pub_api__cpl__123"
	telegram := postings(search.Freehire, 6, 1)[0]
	telegram.URL = "https://t.me/jobnetworkng/37823"

	provider := &fakeProvider{pages: map[call][]job.Job{
		{source: search.Freehire, text: "sre", offset: 0}: {firstParty, republished, telegram},
	}}

	result := run(t, provider, newMemory(), search.Query{Texts: []string{"sre"}, Count: 2})

	if len(result.Jobs) != 1 {
		t.Fatalf("returned %d jobs, want only the first-party row", len(result.Jobs))
	}
	if result.Jobs[0].Key != firstParty.Key {
		t.Errorf("kept %s, want %s", result.Jobs[0].Key, firstParty.Key)
	}
	if result.Skipped != 2 {
		t.Errorf("skipped = %d, want the two aggregator rows", result.Skipped)
	}
}

// A stream stops after ten pages even when every page is full of decided rows.
func TestStreamPageLimit(t *testing.T) {
	memory := newMemory()
	provider := &fakeProvider{pages: map[call][]job.Job{}}
	for page := range 10 {
		offset := page * 10
		rows := postings(search.Freehire, offset, 10)
		for _, item := range rows {
			memory.blocked[item.Key] = true
		}
		provider.pages[call{source: search.Freehire, text: "sre", offset: offset}] = rows
	}

	result := run(t, provider, memory, search.Query{Texts: []string{"sre"}, Count: 5})

	if result.Exhausted != true {
		t.Errorf("a capped stream must count as exhausted")
	}
	freehire := 0
	for _, item := range provider.calls {
		if item.source == search.Freehire {
			freehire++
		}
	}
	if freehire != 10 {
		t.Errorf("freehire pages = %d, want the cap of 10", freehire)
	}
	if len(result.Jobs) != 0 {
		t.Errorf("returned %d jobs, want none", len(result.Jobs))
	}
}

func TestExhaustion(t *testing.T) {
	result := run(t, &fakeProvider{}, newMemory(), search.Query{Texts: []string{"sre"}, Count: 5})

	if len(result.Jobs) != 0 {
		t.Errorf("returned %d jobs, want none", len(result.Jobs))
	}
	if !result.Exhausted {
		t.Error("every stream was dry, so exhausted should be true")
	}
}

// A failing provider must not stop another stream from contributing.
func TestProviderErrorIsContained(t *testing.T) {
	provider := &fakeProvider{
		pages: map[call][]job.Job{
			{source: search.LinkedIn, text: "sre", offset: 0}: postings(search.LinkedIn, 0, 2),
		},
		errs: map[call]error{
			{source: search.Freehire, text: "sre", offset: 0}: errors.New("429 too many requests"),
		},
	}

	result := run(t, provider, newMemory(), search.Query{Texts: []string{"sre"}, Count: 2})
	if len(result.Jobs) != 2 {
		t.Errorf("returned %d jobs, want 2 from the second stream", len(result.Jobs))
	}
	if len(result.Notes) != 1 {
		t.Errorf("notes = %v, want one provider error", result.Notes)
	}
}

// Offsets are local to the run, so every search starts at zero again.
func TestOffsetsStartAtZeroEachRun(t *testing.T) {
	provider := &fakeProvider{pages: map[call][]job.Job{
		{source: search.Freehire, text: "sre", offset: 0}: postings(search.Freehire, 0, 1),
	}}
	memory := newMemory()

	run(t, provider, memory, search.Query{Texts: []string{"sre"}, Count: 1})
	run(t, provider, memory, search.Query{Texts: []string{"sre"}, Count: 1})

	if len(provider.calls) != 2 {
		t.Fatalf("calls = %v, want two runs", provider.calls)
	}
	for _, item := range provider.calls {
		if item.offset != 0 {
			t.Errorf("offset = %d, want 0 on every run", item.offset)
		}
	}
}
