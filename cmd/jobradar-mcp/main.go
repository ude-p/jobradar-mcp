// Command jobradar-mcp is an MCP server that searches job boards through
// openings-mcp and remembers what it has already shown.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"jobradar/internal/search"
	"jobradar/internal/store"
	"jobradar/internal/tools"
)

const version = "v0.1.0"

const instructions = `Jobradar searches job boards and remembers what it has shown.

Use the tools in this order:
1. Call search with one or more phrasings, and the filters you need (country, location, remote, posted_within_days). Two or three phrasings of one role work better than one narrow phrase.
2. Read each posting's body, which comes back with the search result.
3. Judge the postings against the criteria.
4. Call save once with the accepted and rejected keys.

search returns only postings you have not decided on. An empty jobs list with exhausted true means the providers had nothing more for those words, so change the query instead of repeating it. A non-empty notes list means a provider failed and the list is partial.

Keys come from search. Never invent one, and never pass a provider id where a key is expected.`

func main() {
	log.SetFlags(0)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	database := filepath.Join(home, ".config", "jobradar", "seen.db")
	if err := os.MkdirAll(filepath.Dir(database), 0o755); err != nil {
		log.Fatal(err)
	}

	memory, err := store.Open(database)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = memory.Close()
	}()

	client, err := search.Dial(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		_ = client.Close()
	}()

	// Upstream tool names are a public contract we cannot see break, so a
	// rename fails here instead of every search returning nothing.
	if err := client.Verify(ctx); err != nil {
		log.Fatal(err)
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "jobradar", Title: "Jobradar", Version: version},
		&mcp.ServerOptions{Instructions: instructions},
	)
	engine := search.New(memory, client)
	tools.Register(server, tools.Deps{
		Engine:   engine,
		Memory:   memory,
		Database: database,
	})

	log.Printf("jobradar %s: db=%s", version, database)
	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
