set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

# Install the server to $GOPATH/bin.
install:
    go install ./cmd/jobradar-mcp

test:
    go test ./...

tidy:
    go mod tidy

# Print the tool schema the model receives.
schema:
    printf '%s\n' \
      '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"schema","version":"1"}}}' \
      '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}' \
      | "$(go env GOPATH)/bin/jobradar-mcp" 2>/dev/null | tail -1 | python3 -m json.tool
