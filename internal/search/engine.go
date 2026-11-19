package search

import (
	"context"
	"fmt"
	"strings"

	"jobradar/internal/job"
)

type Query struct {
	// Texts are the phrasings, in the caller's order.
	Texts            []string
	Count            int
	Country          string
	Location         string
	Remote           bool
	PostedWithinDays int
}

type Result struct {
	Jobs      []job.Job
	Examined  int
	Skipped   int
	Exhausted bool
	Notes     []string
}

// Memory is the part of the store the engine needs.
type Memory interface {
	Blocked(ctx context.Context, key, urlKey string) (bool, error)
	Touch(ctx context.Context, postings []job.Job) error
}

// Provider is the part of the openings client the engine needs.
type Provider interface {
	Search(ctx context.Context, source string, request Request, offset int) ([]job.Job, error)
}

type Engine struct {
	memory Memory
	source Provider
}

func New(memory Memory, source Provider) *Engine {
	return &Engine{memory: memory, source: source}
}

const (
	DefaultCount            = 10
	DefaultPostedWithinDays = 14
	maxResults              = 100
	linkedInPageSize        = 10
	maxPhrases              = 5
	maxPagesPerStream       = 10
)

// stream is one phrasing on one provider. Its offset lives only for this run.
type stream struct {
	source string
	text   string
	offset int
	pages  int
	dry    bool
}

// Search fills one global target across every stream. It takes the first page
// of each stream first, then keeps paging the ones still producing.
func (e *Engine) Search(ctx context.Context, query Query) (Result, error) {
	normalize(&query)

	result := Result{Jobs: make([]job.Job, 0, query.Count)}
	deduped := make(map[string]struct{}, query.Count)

	streams := make([]stream, 0, len(Names())*len(query.Texts))
	for _, source := range Names() {
		for _, text := range query.Texts {
			streams = append(streams, stream{source: source, text: text})
		}
	}
	if len(streams) == 0 {
		return result, nil
	}

	for index := range streams {
		if complete(query, result) {
			break
		}
		if err := e.page(ctx, &streams[index], query, deduped, &result); err != nil {
			return result, err
		}
	}

	for !complete(query, result) {
		progress := false
		for index := range streams {
			if complete(query, result) {
				break
			}
			if streams[index].dry {
				continue
			}
			if err := e.page(ctx, &streams[index], query, deduped, &result); err != nil {
				return result, err
			}
			progress = true
		}
		if !progress {
			break
		}
	}

	result.Exhausted = true
	for _, item := range streams {
		if !item.dry {
			result.Exhausted = false
		}
	}

	return result, e.memory.Touch(ctx, result.Jobs)
}

func normalize(query *Query) {
	if query.Count <= 0 {
		query.Count = DefaultCount
	}
	if query.Count > maxResults {
		query.Count = maxResults
	}
	if query.PostedWithinDays <= 0 {
		query.PostedWithinDays = DefaultPostedWithinDays
	}
	seen := make(map[string]struct{}, len(query.Texts))
	phrases := make([]string, 0, len(query.Texts))
	for _, text := range query.Texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		phrases = append(phrases, text)
		if len(phrases) == maxPhrases {
			break
		}
	}
	query.Texts = phrases
}

// complete is the global target: only eligible jobs count towards it.
func complete(query Query, result Result) bool {
	return len(result.Jobs) >= query.Count
}

func (e *Engine) page(ctx context.Context, item *stream, query Query, deduped map[string]struct{}, result *Result) error {
	request := Request{
		Query:            item.text,
		Country:          query.Country,
		Location:         query.Location,
		Remote:           query.Remote,
		PostedWithinDays: query.PostedWithinDays,
		PageSize:         query.pageSize(item.source),
	}

	item.pages++
	rows, err := e.source.Search(ctx, item.source, request, item.offset)
	if err != nil {
		// One provider failing must not cost the rows another already found.
		result.Notes = append(result.Notes, fmt.Sprintf("%s %q: %v", item.source, item.text, err))
		item.dry = true
		return nil
	}
	if len(rows) == 0 {
		item.dry = true
		return nil
	}

	for _, posting := range rows {
		if complete(query, *result) {
			break
		}
		result.Examined++

		// A republished copy carries a tracker link and usually arrives next to
		// the first-party row, so drop it before it can occupy a slot.
		if republished(posting.URL) {
			result.Skipped++
			continue
		}

		urlKey := job.URLKey(posting.URL)
		if _, duplicate := deduped[urlKey]; duplicate {
			result.Skipped++
			continue
		}
		deduped[urlKey] = struct{}{}

		blocked, err := e.memory.Blocked(ctx, posting.Key, urlKey)
		if err != nil {
			return err
		}
		if blocked {
			result.Skipped++
			continue
		}

		result.Jobs = append(result.Jobs, posting)
	}

	item.offset += len(rows)
	// A short page ends the stream, and so does reaching the page cap, so a
	// query whose results are all decided cannot page without limit.
	if len(rows) < request.PageSize || item.pages >= maxPagesPerStream {
		item.dry = true
	}
	return nil
}

// pageSize is what one request asks for. LinkedIn returns ten whatever you ask,
// so asking for more would make every page look short and end the stream.
func (query Query) pageSize(source string) int {
	if source == LinkedIn {
		return linkedInPageSize
	}
	return min(max(query.Count*2, 10), maxResults)
}
