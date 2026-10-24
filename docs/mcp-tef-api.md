# mcp-tef API notes

What tefrun relies on from mcp-tef. Verified against the source code at
commit `1553c31` (2026-03-26), not just `docs/openapi.yaml`, which is out of
date in several places (see [Spec drift](#spec-drift)).

Base URL: `http://127.0.0.1:<port>` (started with `--tls-enabled=false`).

## Flow

```
POST /test-cases              -> 201, test case with "id"      (once per case)
POST /test-cases/{id}/run     -> 201, test run, status pending (once per case × model)
GET  /test-runs/{run_id}      -> 200, test run                 (poll until completed/failed)
```

The run call returns immediately; mcp-tef executes the run in a background
task. Status moves `pending -> running -> completed | failed`. A run can also
go straight from `pending` to `failed` if tool ingestion fails.

## POST /test-cases

Request body (`TestCaseCreate`), sent as written in the test file:

| field | type | notes |
|---|---|---|
| `name` | string | required, non-empty |
| `query` | string | required, non-empty |
| `expected_tool_calls` | array or null | null or `[]` = expect no tool call |
| `expected_tool_calls[].mcp_server_url` | string | required |
| `expected_tool_calls[].tool_name` | string | required |
| `expected_tool_calls[].parameters` | object or null | null = don't check parameters |
| `order_dependent_matching` | bool | default `false` |
| `available_mcp_servers` | array, min 1 | each item is a URL string or `{"url", "transport"}`; a string becomes `{"url": s, "transport": "streamable-http"}` |

Creating a test case **contacts the MCP servers**: it checks that each server
is reachable and that each expected tool exists on its server. If not, it
fails with 422.

Response `201`: the test case with `id` (UUID), the fields above,
`created_at`, `updated_at`.

## POST /test-cases/{test_case_id}/run

Header: `X-Model-API-Key: <key>`, optional; omitted for keyless providers
such as ollama. Not stored by mcp-tef.

Request body (`TestRunExecuteRequest`):

```json
{
  "model_settings": {
    "provider": "openrouter",
    "model": "anthropic/claude-3.5-sonnet",
    "timeout": 30,
    "temperature": 0.4,
    "max_retries": 3
  }
}
```

| field | type | default | limits |
|---|---|---|---|
| `provider` | string | required | 1–100 chars |
| `model` | string | required | 1–100 chars |
| `timeout` | int (seconds) | 30 | 1–300 |
| `temperature` | float | 0.4 | 0.0–2.0 |
| `max_retries` | int | 3 | 0–10 |
| `base_url` | string or null | null | mcp-tef fills in a default for ollama; openrouter needs none |
| `system_prompt` | string or null | null | |

Response `201`: a test run (shape below) with `status: "pending"`.
Unknown test case id: 404. Invalid body: 422.

## GET /test-runs/{test_run_id}

Response `200` (`TestRunResponse`):

| field | type | notes |
|---|---|---|
| `id` | string | test run id |
| `test_case_id` | string | |
| `status` | string | `pending`, `running`, `completed`, `failed` |
| `classification` | string or null | `TP`, `FP`, `TN`, `FN`; set when completed |
| `llm_confidence` | string or null | `high` / `low`, as reported by the LLM |
| `confidence_score` | string or null | `robust description`, `needs clarity`, `misleading description` |
| `avg_parameter_correctness` | float or null | 0–10, average over TP matches |
| `execution_time_ms` | int or null | measured by mcp-tef, includes tool ingestion |
| `error_message` | string or null | why a run failed |
| `tool_call_matches` | array | see below |
| `llm_response_raw` | string or null | full LLM conversation as text: prompts, tool calls, retries, replies |
| `tools` | array | every tool offered to the LLM |
| `model_settings` | object | settings used, plus `id` and `created_at`; never the API key |
| `created_at`, `completed_at` | datetime | `completed_at` null until done |

Each `tool_call_matches[]` item:

| field | type | notes |
|---|---|---|
| `match_type` | string | `TP`, `FP`, `FN`, `TN` |
| `expected_tool_call` | object or null | null for FP |
| `actual_tool_call` | object or null | tool the LLM picked (`name`, `mcp_server_url`, ...); null for FN |
| `actual_parameters` | object or null | arguments the LLM used |
| `parameter_correctness` | float or null | 0–10 |
| `parameter_justification` | string or null | explanation of the score |

Unknown run id: 404.

## Errors

mcp-tef errors use this body:

```json
{ "error": "ResourceNotFoundError", "message": "...", "details": null }
```

Request validation errors from FastAPI (422) use `{"detail": [...]}` instead.
Unexpected errors return 500 with `"error": "InternalServerError"`.

## Not available

- **Token usage**: not recorded or returned. Pydantic AI has it internally;
  mcp-tef discards it.
- **Retry count**: not recorded or returned. Only `max_retries` is passed to
  the LLM client. Retries appear as text in `llm_response_raw`.
- **Reasoning / recommendations per run**: no endpoint. tefrun shows
  `parameter_justification` and `llm_response_raw` instead.

Plan: add token usage and retry count in a fork of mcp-tef, and read them
when present.

## Spec drift

Where `docs/openapi.yaml` disagrees with the code:

| | openapi.yaml | code |
|---|---|---|
| Run response | `200`, completed result | `201`, `status: "pending"` |
| Run body key | `model_config` | `model_settings` |
| `GET /test-runs/{id}/result` | exists, returns reasoning and recommendations | does not exist |
| `TestRunResponse` | no `tool_call_matches`, `llm_response_raw`, `tools` | has them |
| `ModelSettingsCreate` | no `base_url`, `system_prompt` | has them |

`docs/quickstart.md` uses `model_settings` correctly but still calls
`/result`. `docs/data-model.md` describes an `evaluation_results` table with
`reasoning`; `storage/schema.sql` has no such table.
