# tefrun

Run MCP tool-selection evals against a local [mcp-tef](https://github.com/StacklokLabs/mcp-tef) server.

## Known limitations

- **Parallel tool calls:** mcp-tef fails when a model makes several tool calls in one turn ("Tool return found without corresponding tool call"). A GitHub case that needed two independent calls failed on every model, so we dropped it. Other cases can still fail this way when a model chooses to call tools in parallel.
- **Provider routing:** OpenRouter spreads DeepSeek requests across many providers and quantizations, so results changed a lot between runs. `examples/github/model.config.json` pins DeepSeek with the OpenRouter preset `deepseek-mcp-eval` (only DeepInfra, fp8, no fallbacks). Create that preset in your account, or remove the `@preset/...` suffix.
