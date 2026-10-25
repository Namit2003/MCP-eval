# tefrun

Run MCP tool-selection evals against a local [mcp-tef](https://github.com/StacklokLabs/mcp-tef) server.

## Known limitations

- **Parallel tool calls:** mcp-tef fails when a model makes several tool calls in one turn ("Tool return found without corresponding tool call"). A GitHub case that needed two independent calls failed on every model, so we dropped it. Other cases can still fail this way when a model chooses to call tools in parallel.
