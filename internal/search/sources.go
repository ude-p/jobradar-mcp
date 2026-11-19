package search

import (
	"net/url"
	"strings"
)

// blockedOrigins are freehire source slugs that republish other boards' jobs.
// They are sent as source_exclude so those rows never arrive.
var blockedOrigins = []string{
	"adzuna",
	"arbeitnow",
	"djinni",
	"eures",
	"getmatch",
	"getonbrd",
	"habr_career",
	"himalayas",
	"jobleads",
	"jobstreet",
	"jobtech",
	"mycareersfuture",
	"powertofly",
	"startupjobs",
	"telegram",
	"torre",
	"whatjobs",
	"wttj",
}

// blockedLabels are brand names that never host a first-party posting. Matching
// a label, rather than a full host, catches the regional domains such as
// pt.whatjobs.com and www.careerjet.co.uk.
var blockedLabels = map[string]struct{}{
	"adzuna":      {},
	"careerjet":   {},
	"jobisjob":    {},
	"jobrapido":   {},
	"jobted":      {},
	"jooble":      {},
	"learn4good":  {},
	"neuvoo":      {},
	"simplyhired": {},
	"whatjobs":    {},
}

// blockedHosts are exact hosts, for the ones a label check cannot catch.
var blockedHosts = map[string]struct{}{
	"t.me": {},
}

var blockedSourceQuery = strings.Join(blockedOrigins, ",")

// republished reports whether a posting URL points at an aggregator rather than
// at the employer or the board that first published it.
func republished(rawURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" {
		return false
	}
	if _, blocked := blockedHosts[host]; blocked {
		return true
	}
	for _, label := range strings.Split(host, ".") {
		if _, blocked := blockedLabels[label]; blocked {
			return true
		}
	}
	return false
}
