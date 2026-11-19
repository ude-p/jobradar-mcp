// Package tools registers the MCP tools the model calls.
package tools

import (
	"context"
	"errors"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jobradar/internal/job"
	"jobradar/internal/search"
	"jobradar/internal/store"
)

type SearchInput struct {
	Queries          []string `json:"queries" jsonschema:"One or more phrasings of what to look for, for example platform engineer and site reliability engineer. Results are unioned and duplicates removed."`
	Count            int      `json:"count,omitempty" jsonschema:"How many postings to return. Default 10, maximum 100."`
	Country          string   `json:"country,omitempty" jsonschema:"ISO 3166-1 alpha-2 country code, lowercase, for example ng or de. Omit for worldwide."`
	Location         string   `json:"location,omitempty" jsonschema:"Place name for the boards that only take free text, for example Lagos, Nigeria. Omit to search worldwide."`
	Remote           bool     `json:"remote,omitempty" jsonschema:"Return only postings that state remote work."`
	PostedWithinDays int      `json:"posted_within_days,omitempty" jsonschema:"Only postings newer than this many days. Default 14."`
}

type SearchOutput struct {
	Jobs      []job.Job `json:"jobs" jsonschema:"Postings you have not decided on yet."`
	Exhausted bool      `json:"exhausted" jsonschema:"True when the providers ran out of postings for this query, so a second call adds nothing."`
	Notes     []string  `json:"notes,omitempty" jsonschema:"Provider errors that did not stop the run. When present, the list is partial."`
}

type DetailInput struct {
	Source string `json:"source" jsonschema:"The source field of the posting, for example freehire or linkedin."`
	ID     string `json:"id" jsonschema:"The id field of the posting from jobradar_search."`
}

type DetailOutput struct {
	Job job.Job `json:"job" jsonschema:"The posting with its full description, which search only previews."`
}

type SaveInput struct {
	Approved []string `json:"approved,omitempty" jsonschema:"Keys of postings that fit. They are not returned again."`
	Rejected []string `json:"rejected,omitempty" jsonschema:"Keys of postings that do not fit. They are not returned again."`
}

type SaveOutput struct {
	Approved []job.Job `json:"approved" jsonschema:"The approved postings, ready to report."`
	Rejected int       `json:"rejected" jsonschema:"Rows marked rejected."`
	Unknown  int       `json:"unknown" jsonschema:"Keys that were not in the store, which means they were invented or came from another tool."`
}

type StatusInput struct{}

// Deps is what the tools need from the rest of the program.
type Deps struct {
	Engine   *search.Engine
	Client   *search.Client
	Memory   *store.Store
	Database string
}

type StatusOutput struct {
	Database string         `json:"database"`
	Counts   map[string]int `json:"counts"`
}

func Register(server *mcp.Server, deps Deps) {
	mcp.AddTool(server, &mcp.Tool{
		Name:  "search",
		Title: "Search job postings",
		Description: "Search job boards and company career sites, and return only postings this session has not shown you and you have not decided on. " +
			"Pass two or three phrasings when the role has several names; the tool pools them and drops duplicates. " +
			"The tool pages the providers internally until it has count postings, so one call is usually enough. " +
			"Results carry a preview only: fetch the ones worth reading with get_detail before judging. " +
			"Then report what you decided with save, otherwise the same postings come back.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, OpenWorldHint: new(true)},
	}, searchTool(deps.Engine))

	mcp.AddTool(server, &mcp.Tool{
		Name:  "get_detail",
		Title: "Read one posting",
		Description: "Return the full body of one posting by the source and id that search returned. " +
			"Use it only for postings worth judging: it costs a provider request per call.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(true)},
	}, detailTool(deps.Client))

	mcp.AddTool(server, &mcp.Tool{
		Name:  "save",
		Title: "Record your verdicts",
		Description: "Record which postings fit and which do not, by the key field from search. " +
			"Call it once after judging a batch. The approved postings come back, so report them from here. " +
			"An unsaved posting can be shown to you again, and a saved one never is.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: false, OpenWorldHint: new(false)},
	}, saveTool(deps.Memory))

	mcp.AddTool(server, &mcp.Tool{
		Name:        "status",
		Title:       "Memory status",
		Description: "Report the size of the posting memory and the database file. Use it when a search returns nothing, to check whether an empty result means no memory or no matches.",
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: new(false)},
	}, statusTool(deps.Memory, deps.Database))
}

func searchTool(engine *search.Engine) mcp.ToolHandlerFor[SearchInput, SearchOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input SearchInput) (*mcp.CallToolResult, SearchOutput, error) {
		if len(input.Queries) == 0 {
			return nil, SearchOutput{}, errors.New("queries is required")
		}

		result, err := engine.Search(ctx, search.Query{
			Texts:            input.Queries,
			Count:            input.Count,
			Country:          input.Country,
			Location:         input.Location,
			Remote:           input.Remote,
			PostedWithinDays: input.PostedWithinDays,
		})
		if err != nil {
			return nil, SearchOutput{}, err
		}
		return nil, SearchOutput{
			Jobs:      result.Jobs,
			Exhausted: result.Exhausted,
			Notes:     result.Notes,
		}, nil
	}
}

func detailTool(client *search.Client) mcp.ToolHandlerFor[DetailInput, DetailOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input DetailInput) (*mcp.CallToolResult, DetailOutput, error) {
		posting, err := client.Detail(ctx, input.Source, input.ID)
		if err != nil {
			return nil, DetailOutput{}, err
		}
		return nil, DetailOutput{Job: posting}, nil
	}
}

func saveTool(memory *store.Store) mcp.ToolHandlerFor[SaveInput, SaveOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, input SaveInput) (*mcp.CallToolResult, SaveOutput, error) {
		marked, err := memory.Decide(ctx, input.Approved, store.Approved)
		if err != nil {
			return nil, SaveOutput{}, err
		}
		rejected, err := memory.Decide(ctx, input.Rejected, store.Rejected)
		if err != nil {
			return nil, SaveOutput{}, err
		}
		approved, err := memory.ApprovedJobs(ctx, input.Approved)
		if err != nil {
			return nil, SaveOutput{}, err
		}

		unknown := max(0, len(input.Approved)+len(input.Rejected)-marked-rejected)
		return nil, SaveOutput{Approved: approved, Rejected: rejected, Unknown: unknown}, nil
	}
}

func statusTool(memory *store.Store, database string) mcp.ToolHandlerFor[StatusInput, StatusOutput] {
	return func(ctx context.Context, _ *mcp.CallToolRequest, _ StatusInput) (*mcp.CallToolResult, StatusOutput, error) {
		counts, err := memory.Counts(ctx)
		if err != nil {
			return nil, StatusOutput{}, err
		}
		return nil, StatusOutput{Database: database, Counts: counts}, nil
	}
}
