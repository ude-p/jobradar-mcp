// Package search fetches postings through openings-mcp and picks the ones the
// model has not decided on yet.
package search

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jobradar/internal/job"
)

const (
	Freehire = "freehire"
	LinkedIn = "linkedin"
)

// Request is the source-neutral query that each provider maps into its own
// arguments.
type Request struct {
	Query            string
	Country          string
	Location         string
	Remote           bool
	PostedWithinDays int
	PageSize         int
}

type provider struct {
	searchTool string
	arguments  func(Request, int) map[string]any
}

var providers = map[string]provider{
	Freehire: {
		searchTool: "freehire_search_jobs",
		arguments:  freehireArguments,
	},
	LinkedIn: {
		searchTool: "linkedin_search_jobs",
		arguments:  linkedInArguments,
	},
}

func Names() []string {
	return []string{Freehire, LinkedIn}
}

func freehireArguments(request Request, offset int) map[string]any {
	arguments := map[string]any{
		"limit":          request.PageSize,
		"offset":         offset,
		"reality":        []string{"fresh"},
		"source_exclude": blockedSourceQuery,
	}
	if request.Query != "" {
		arguments["q"] = request.Query
	}
	if request.Country != "" {
		arguments["countries"] = strings.ToLower(request.Country)
	}
	if request.Remote {
		arguments["work_mode"] = []string{"remote"}
	}
	if request.PostedWithinDays > 0 {
		arguments["posted_within_days"] = request.PostedWithinDays
	}
	return arguments
}

func linkedInArguments(request Request, offset int) map[string]any {
	arguments := map[string]any{"start": offset}
	if request.Query != "" {
		arguments["keyword"] = request.Query
	}
	if request.Location != "" {
		arguments["location"] = request.Location
	}
	if request.Remote {
		arguments["workplace_type"] = "Remote"
	}

	// The guest surface offers two windows, not an arbitrary day count.
	switch {
	case request.PostedWithinDays > 7:
		arguments["posted_within"] = "Past month"
	case request.PostedWithinDays > 0:
		arguments["posted_within"] = "Past week"
	}
	return arguments
}

type Client struct {
	session *mcp.ClientSession
}

// Dial starts the openings-mcp server and connects to it over stdio.
func Dial(ctx context.Context) (*Client, error) {
	if _, err := exec.LookPath("go"); err != nil {
		return nil, fmt.Errorf("the go toolchain is required to start openings-mcp: %v", err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "jobradar", Version: "v0.1.0"}, nil)
	transport := &mcp.CommandTransport{Command: exec.Command("go", "run", "github.com/amikai/openings-mcp/cmd/openings-mcp@v0.17.0")}
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("start openings-mcp: %v", err)
	}
	return &Client{session: session}, nil
}

func (c *Client) Close() error {
	return c.session.Close()
}

// Verify fails when a tool this program calls is missing, so an upstream rename
// breaks startup instead of every search.
func (c *Client) Verify(ctx context.Context) error {
	available := make(map[string]struct{})
	for tool, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return fmt.Errorf("list tools: %v", err)
		}
		available[tool.Name] = struct{}{}
	}

	var missing []string
	for _, name := range Names() {
		entry := providers[name]
		if _, ok := available[entry.searchTool]; !ok {
			missing = append(missing, entry.searchTool)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("openings-mcp is missing tools: %s", strings.Join(missing, ", "))
	}
	return nil
}

func (c *Client) Search(ctx context.Context, source string, request Request, offset int) ([]job.Job, error) {
	entry, ok := providers[source]
	if !ok {
		return nil, fmt.Errorf("unknown source %q", source)
	}

	result, err := c.session.CallTool(ctx, &mcp.CallToolParams{
		Name:      entry.searchTool,
		Arguments: entry.arguments(request, offset),
	})
	if err != nil {
		return nil, fmt.Errorf("%s: %v", entry.searchTool, err)
	}
	if result.IsError {
		return nil, fmt.Errorf("%s: %s", entry.searchTool, contentText(result))
	}

	rows, err := data(result)
	if err != nil {
		return nil, fmt.Errorf("%s: %v", entry.searchTool, err)
	}

	postings := make([]job.Job, 0, len(rows))
	for _, row := range rows {
		if posting, ok := parse(source, row); ok {
			postings = append(postings, posting)
		}
	}
	return postings, nil
}

func object(result *mcp.CallToolResult) (map[string]any, error) {
	structured, ok := result.StructuredContent.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("result has no structured content")
	}
	return structured, nil
}

func data(result *mcp.CallToolResult) ([]map[string]any, error) {
	envelope, err := object(result)
	if err != nil {
		return nil, err
	}

	raw, ok := envelope["data"].([]any)
	if !ok {
		return nil, nil
	}
	rows := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if row, ok := item.(map[string]any); ok {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

func contentText(result *mcp.CallToolResult) string {
	var builder strings.Builder
	for _, content := range result.Content {
		if block, ok := content.(*mcp.TextContent); ok {
			builder.WriteString(block.Text)
		}
	}
	return builder.String()
}
