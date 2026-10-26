# tefrun

Run MCP tool-selection evals against a local [mcp-tef](https://github.com/StacklokLabs/mcp-tef) server.

## GitHub benchmark

mcp-tef can't send auth headers, so `tefrun proxy` adds your GitHub token and forwards to GitHub's remote MCP server.

1. Create a [fine-grained token](https://github.com/settings/personal-access-tokens/new) with **Repository access: Public repositories**. The read-only cases need no extra permissions.
2. Export your keys:
   ```sh
   export GITHUB_PAT=...
   export OPENROUTER_API_KEY=...
   ```
3. Start the proxy (`--rate 2` avoids GitHub 429s):
   ```sh
   tefrun proxy --target https://api.githubcopilot.com/mcp/readonly --token-env GITHUB_PAT --rate 2
   ```
4. In another terminal, run the benchmark:
   ```sh
   tefrun run --tests examples/github/evals --models examples/github/model.config.json --parallel 2 --timeout 60m --out reports/github
   ```

A sample report is in `examples/github/sample-report/`.

## Known limitations

- **Parallel tool calls:** mcp-tef fails when a model makes several tool calls in one turn ("Tool return found without corresponding tool call"). A GitHub case that needed two independent calls failed on every model, so we dropped it. Other cases can still fail this way when a model chooses to call tools in parallel.
- **Provider routing:** OpenRouter spreads DeepSeek requests across many providers and quantizations, so results changed a lot between runs. `examples/github/model.config.json` pins DeepSeek with the OpenRouter preset `deepseek-mcp-eval` (only DeepInfra, fp8, no fallbacks). Create that preset in your account, or remove the `@preset/...` suffix.
