# jobradar

An MCP server that searches job boards and remembers what it has shown. It runs
one search tool family over [openings-mcp](https://github.com/amikai/openings-mcp)
and keeps the postings you decided on in SQLite.

The forwarding part is a proxy. To the client it is an MCP server. To
openings-mcp it is an MCP client.

## What it adds

openings-mcp answers one query at a time and stores nothing. This server adds
the memory that a repeated search needs.

- A posting you approved or rejected never comes back.
- A posting shown but not decided comes back on a later search, so an
  interrupted run loses nothing.
- The same posting from two providers is returned once, because the same URL
  has one identity.
- Paging happens inside the tool. One call returns the count you asked for.

## Tools

| tool | input | output |
| --- | --- | --- |
| `search` | `queries[]`, `count`, `country`, `location`, `remote`, `posted_within_days` | postings not yet decided, with a preview, plus `exhausted` and `notes` |
| `get_detail` | `source`, `id` | one posting with its full description |
| `save` | `approved[]`, `rejected[]` of `key` values | the approved postings, plus counts |
| `status` | none | database path and counts by decision |

The model uses them in this order: `search`, then `get_detail` for the postings
worth reading, then `save` with its verdicts. A posting that the model never
saves stays undecided, so a later search can return it again.

## Requirements

Go 1.27 or newer. The toolchain is not optional: the server starts
openings-mcp with `go run github.com/amikai/openings-mcp/cmd/openings-mcp@v0.17.0`,
so it needs `go` on `PATH` and fails at startup without it.

The first start downloads and builds the pinned version. Every start after that
uses the module and build cache, which is fast.

## Install

```sh
go install ./cmd/jobradar-mcp
```

That writes `jobradar-mcp` to `$(go env GOPATH)/bin`, which is where the MCP
configuration points.

## Connect a client

Add this entry to the MCP configuration. The `pi` path is
`~/.config/pi/mcp.json`.

```json
{
  "mcpServers": {
    "jobradar": {
      "command": "/Users/Ude/.golang/bin/jobradar-mcp",
      "description": "Search job boards with memory: returns postings you have not decided on yet."
    }
  }
}
```

Turn off direct access to `openings` for the same server list when you use this
one. Otherwise a model can call `freehire_search_jobs` directly and get
postings that are already decided, which starts the duplicate problem again.

## Configuration

No environment variables. The memory file is at `~/.config/jobradar/seen.db`,
under the home directory the process runs with.

## The memory

A posting with a decision, approved or rejected, is never shown again. A posting
with no decision was seen but never judged, so it is not blocked and can come
back on a later search.

Search positions are not stored. Every search starts at the first result of each
stream and walks down until the target is met or every stream runs out. A stream
stops after ten pages, so a query whose results are all decided cannot page
without limit.

Identity is two values. The key is `source:id`, which is what the model saves.
The URL key is the posting URL without the query string, the fragment and the
host case, hashed. The URL key stops the same posting from two providers.

Providers are tried in this order:

1. `freehire` searches every ATS board that freehire crawls, and it is free.
2. `linkedin` searches the public guest surface, which returns ten postings per
   call.

Only these two are wired. The openings-mcp company tools are company scoped, so
they do not fit a keyword search. A provider is added in
`internal/search/client.go` as one table entry with its tool names and its
argument mapping.

## Layout

```text
cmd/jobradar-mcp   the server: config, wiring, stdio transport
internal/job       the posting shape
internal/search    MCP client, row mapping, streams, paging, dedupe
internal/store     SQLite: jobs and decisions
internal/tools     the four tool contracts
tests/unit         engine tests with a fake provider and memory
```

## Tests

```sh
go test ./...
```

The store tests use a temporary database. The engine tests use a fake provider,
so they need no network.
