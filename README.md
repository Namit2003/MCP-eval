# tefrun

Test how well LLMs pick the right tool on an MCP server. tefrun runs every test case on every model through a local [mcp-tef](https://github.com/StacklokLabs/mcp-tef) server and writes JSON, HTML and terminal reports.

It works with any MCP server reachable over HTTP. Servers, test cases, and setup/cleanup steps are all defined in JSON, with no code changes.

## Prerequisites

- Go 1.27+
- git and [uv](https://docs.astral.sh/uv/) (mcp-tef needs Python 3.13+; uv installs it). tefrun clones, installs and starts mcp-tef itself.
- An API key for each model provider you use (for example `OPENROUTER_API_KEY`). Ollama needs none.

## Install

```sh
git clone https://github.com/Namit2003/mcp-eval && cd mcp-eval
make build   # or: go build -o bin/tefrun .
```

`make test` runs the tests and `make lint` runs `go vet` plus a gofmt check.

## Quick start

```sh
tefrun validate --tests 'examples/evals/**/*.json' --models examples/model.config.json
tefrun run      --tests 'examples/evals/**/*.json' --models examples/model.config.json --out reports
```

`validate` checks every file, model API key and step `auth_env` without running anything. `run` writes `results.json`, `report.html` and `mcp-tef.log` to `--out`. Each run starts with a fresh mcp-tef database.

## File formats

### Model config

```json
{
  "models": [
    { "provider": "openrouter", "model": "anthropic/claude-haiku-5.5", "api_key_env": "OPENROUTER_API_KEY", "temperature": 0, "timeout": 60 },
    { "provider": "ollama", "model": "llama3.2:3b" }
  ]
}
```

The API key is read from the env var named by `api_key_env` and sent to mcp-tef per request. It is never stored or logged. mcp-tef stores `timeout` and `max_retries` but does not use them.

### Test file

A test file holds `{"test_cases": [...]}`. Each case uses mcp-tef's test case fields:

```json
{
  "name": "Get tag",
  "query": "Show the details of the v1.10.0 git tag in spf13/cobra",
  "expected_tool_calls": [
    { "mcp_server_url": "http://127.0.0.1:8080", "tool_name": "get_tag",
      "parameters": { "owner": "spf13", "repo": "cobra", "tag": "v1.10.0" } }
  ],
  "order_dependent_matching": false,
  "available_mcp_servers": ["http://127.0.0.1:8080"]
}
```

An empty `expected_tool_calls` means the model should call no tool.

tefrun adds these, all optional:

- **Variables:** `${name}` inside any string is filled from `--var name=value`. An undefined variable fails loading.
- **`${job}`:** a value unique to each job (`<run start>-<job number>`). Use it to name things a job creates, so parallel jobs don't collide.
- **`setup` / `teardown`:** HTTP steps run before and after each job. Teardown always runs, even after a failure or Ctrl+C. It skips any step that uses a variable that was never saved, since that thing was never created. Both are removed before the case is sent to mcp-tef.

```json
"setup": [
  { "method": "POST", "url": "https://api.github.com/repos/${owner}/${repo}/issues",
    "auth_env": "GITHUB_WRITE_PAT", "body": { "title": "tefrun ${job}" },
    "save": { "issue": "number" } }
],
"teardown": [
  { "method": "PATCH", "url": "https://api.github.com/repos/${owner}/${repo}/issues/${issue}",
    "auth_env": "GITHUB_WRITE_PAT", "body": { "state": "closed" } }
]
```

| Step field | Meaning |
|---|---|
| `method`, `url` | The request. Any non-2xx response fails the step. |
| `body` | Optional JSON body. |
| `auth_env` | Optional. Sends `Authorization: Bearer <value of this env var>`. |
| `save` | `{name: path}`. Saves a value from the JSON response, for example `number` or `items.0.number`, as `${name}` for the rest of the job. |
| `retry` | Optional. Extra attempts, 2s apart, if the step fails. |

A setup failure fails the job without running the model. A teardown failure keeps the job's result, is recorded as `teardown_error` and is printed as a warning so you can clean up by hand.

## Commands

### `run`

| Flag | Default | |
|---|---|---|
| `--tests` | required | Test files, directories or globs (`**` supported). Repeatable. Quote globs. |
| `--models` | required | Model config file. |
| `--var` | | `name=value` for `${name}`. Repeatable. |
| `--parallel` | 4 | Max jobs at once, across all models. |
| `--timeout` | 30m | Deadline for the whole run. |
| `--job-timeout` | 10m | Deadline for each job, excluding teardown. |
| `--poll-interval` | 2s | How often to check a job's status. |
| `--out` | `./reports` | Report directory. |
| `--mcp-tef-dir` | `./.mcp-tef` | Where mcp-tef is cloned. |
| `--mcp-tef-ref` | `main` | mcp-tef git ref. |
| `--port` | 8000 | mcp-tef port. |

### `validate`

Takes `--tests`, `--models` and `--var`. Reports every problem at once.

### `proxy`

Forwards local requests to a remote MCP server and adds a bearer token. Listens on `127.0.0.1` only.

| Flag | Default | |
|---|---|---|
| `--target` | required | Remote MCP server URL. |
| `--token-env` | required | Env var holding the token. |
| `--port` | 8080 | Local port. |
| `--rate` | 0 | Max requests per second to the target. 0 means no limit. |

## Metrics

Each job (one case on one model) is classified by mcp-tef:

| | Meaning |
|---|---|
| **TP** | Made every expected call and nothing else. |
| **FN** | Missed at least one expected call, for example by calling the wrong tool. This wins over FP. |
| **FP** | Made every expected call plus an extra one, or called a tool when none was expected. |
| **TN** | Expected no tool and called none. |

- **Pass rate** = (TP + TN) / all jobs. Failed jobs count against it.
- **Exec time** is measured by mcp-tef. **Wall time** is measured by tefrun, from asking mcp-tef to run the job to its final status, excluding setup and teardown. Both are reported as avg, p50 and p95.

## GitHub benchmark

`examples/github/` has 21 read cases (`evals/`) and 7 write cases (`write/`) for GitHub's remote MCP server.

**Read cases**

1. Create a [fine-grained token](https://github.com/settings/personal-access-tokens/new) with **Repository access: Public repositories**. Then export your keys:
   ```sh
   export GITHUB_PAT=... OPENROUTER_API_KEY=...
   ```
2. Start the proxy:
   ```sh
   tefrun proxy --target https://api.githubcopilot.com/mcp/readonly --token-env GITHUB_PAT --rate 2
   ```
3. Run the benchmark:
   ```sh
   tefrun run --tests examples/github/evals --models examples/github/model.config.json --parallel 2 --timeout 60m --out reports/github
   ```

**Write cases** create and clean up issues, branches and PRs, so use a throwaway repo.

1. Create a repo with a README, then a fine-grained token for **only that repo** with Contents, Issues and Pull requests set to read/write. Export it as `GITHUB_WRITE_PAT`.
2. Start a second proxy on the full endpoint:
   ```sh
   tefrun proxy --target https://api.githubcopilot.com/mcp/ --token-env GITHUB_WRITE_PAT --port 8081 --rate 2
   ```
3. Run with your repo:
   ```sh
   tefrun run --tests examples/github/write --models examples/github/model.config.json --var owner=<you> --var repo=<sandbox> --parallel 2 --out reports/github-write
   ```

A sample report is in `examples/github/sample-report/`.

## CI

`run` exits 0 once the run completes, even if jobs failed. It exits 1 for invalid input, a setup error, or an interrupt. Use `results.json` to gate on scores.

```yaml
- run: go build -o bin/tefrun .
- run: bin/tefrun run --tests 'evals/**/*.json' --models model.config.json --out reports
  env:
    OPENROUTER_API_KEY: ${{ secrets.OPENROUTER_API_KEY }}
- uses: actions/upload-artifact@v4
  with: { name: tefrun-report, path: reports }
```

The runner needs uv installed, for example with `astral-sh/setup-uv`.

## Known limitations

| Problem | What to do |
|---|---|
| mcp-tef runs tools for real. | Point write cases at a throwaway repo or account. |
| mcp-tef can't send auth headers. | Use `tefrun proxy`. |
| GitHub returns 429 under load. | Use `proxy --rate 2`. |
| mcp-tef fails when a model makes several tool calls in one turn ("Tool return found without corresponding tool call"). | No fix. A case that needed two independent calls failed on every model, so it was dropped. Other cases can still hit this. |
| OpenRouter spreads some models across many providers, so results swing between runs. | `examples/github/model.config.json` pins DeepSeek with the OpenRouter preset `deepseek-mcp-eval` (only DeepInfra, fp8, no fallbacks). Create that preset in your account, or remove the `@preset/...` suffix. |
| GitHub search takes a few seconds to index new issues. | Teardown search steps use `retry`. |
| An mcp-tef run can hang, and mcp-tef can't cancel it. | `--job-timeout` fails the job and frees its slot. |
| No token usage or retry counts. | Not available from stock mcp-tef. |
