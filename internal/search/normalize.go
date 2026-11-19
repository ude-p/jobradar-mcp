package search

import (
	"regexp"
	"strings"

	"jobradar/internal/job"
)

// parse maps one provider row into the shared posting shape. A row without an
// identifier, a URL, a title or a company is dropped, because the model cannot
// use it and the store cannot key it.
func parse(source string, row map[string]any) (job.Job, bool) {
	posting := job.Job{Source: source}

	switch source {
	case Freehire:
		posting.ID = text(row["public_slug"])
		posting.URL = text(row["url"])
		posting.Title = text(row["title"])
		posting.Company = text(row["company"])
		posting.Location = text(row["location"])
		posting.Posted = text(row["posted_at"])
		posting.Remote = text(row["work_mode"]) == "remote"
		posting.Preview = trim(text(row["description"]), previewLimit)
	case LinkedIn:
		posting.ID = text(row["id"])
		posting.URL = text(row["url"])
		posting.Title = text(row["title"])
		posting.Company = text(row["company"])
		posting.Location = text(row["location"])
		posting.Posted = text(row["posted_date"])
		if posting.Posted == "" {
			posting.Posted = text(row["posted"])
		}
		posting.Remote = boolean(row["remote"])
		posting.Preview = trim(text(row["description"]), previewLimit)
	default:
		return job.Job{}, false
	}

	if posting.ID == "" || posting.URL == "" || posting.Title == "" || posting.Company == "" {
		return job.Job{}, false
	}
	posting.Key = source + ":" + posting.ID
	return posting, true
}

// previewLimit keeps a search result small; the full body comes from Detail.
const previewLimit = 400

var tags = regexp.MustCompile(`<[^>]*>`)

func text(value any) string {
	raw, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.Join(strings.Fields(tags.ReplaceAllString(raw, " ")), " ")
}

func boolean(value any) bool {
	flag, ok := value.(bool)
	return ok && flag
}

func trim(value string, limit int) string {
	if len(value) <= limit {
		return value
	}

	// Cut on a rune boundary: a byte slice can split a multi-byte character
	// and put invalid UTF-8 into the JSON the model reads.
	cut := 0
	for index := range value {
		if index > limit {
			break
		}
		cut = index
	}
	return strings.TrimSpace(value[:cut]) + "..."
}
