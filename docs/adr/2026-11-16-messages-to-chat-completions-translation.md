# Anthropic Messages to Chat Completions Translation for Non-Claude Bedrock Models

**Status:** Proposed  
**Date:** 2026-11-16  
**Deciders:** Platform Team  
**Technical Story:** [runkora/kora#587](https://github.com/runkora/kora/issues/587)

---

## Context

Claude Code speaks only the Anthropic Messages API (`POST /v1/messages`). It cannot use non-Claude models on Bedrock through the gateway:

*   `providers/bedrock/anthropic.go` returns `MODEL_NOT_SUPPORTED` from `AnthropicMessages` and `AnthropicMessagesStream` when `dispatchBackend(params.Model)` is not `backendAnthropic`.
*   `computeDialects` in `providers/bedrock/models.go` gives every non-Claude text model the `openai` dialect only. The router never selects these models for `/v1/messages`.
*   Bedrock does not serve the Anthropic Messages API for GLM, Kimi or MiniMax. The [Bedrock API compatibility table](https://docs.aws.amazon.com/bedrock/latest/userguide/models-api-compatibility.html) lists it for newer Claude models only. The gateway calls these models through the Bedrock OpenAI-compatible endpoint (`backendOpenAICompat`).

Open models such as GLM 5.3 therefore reach Claude Code agents only through OpenRouter, a `compatible` provider. OpenRouter sends repository code outside the AWS account.

The [zero-translation ADR](2026-04-01-anthropic-zero-translation-path.md) keeps cross-dialect failover out of scope because it is not lossless. Its bugs came from one OpenAI-shaped domain type in the middle of the handler path, which every `/v1/messages` request crossed.

This touches **How does it connect?** The gateway changes the upstream request for one class of model. *Who is this agent?* and *What can it access?* are unchanged.

### Goals

*   A Claude Code agent completes a streaming run with tool calls on a non-Claude Bedrock model.
*   Repository code stays inside Bedrock.
*   Requests for Claude models keep the zero-translation path, unchanged.
*   Every feature that the translation loses is documented in this ADR.

### Non-Goals

*   Lossless translation. It is not possible.
*   Cross-dialect failover. Failover stays within one dialect. Connector-local translation does not enable failover.
*   Running Anthropic server tools for non-Claude models.
*   Responses-only Bedrock models (`backendMantleOpenAIV1`: GPT-5.x and Grok).
*   `/v1/messages/count_tokens`.

---

## Options

### Option A: Keep the refusal

Non-Claude Bedrock models stay unavailable to Claude Code. Agents continue to use OpenRouter, and repository code continues to leave the AWS account.

### Option B: Translate in the connector layer (chosen)

A typed converter maps `anthropic.MessageNewParams` to `openai.ChatCompletionNewParams` and maps the response back. It sits behind the `AnthropicConnector` interface, inside the Bedrock connector. The handler, the service layer and usage recording stay unchanged.

### Option C: Translate in a shared domain type

This option reopens the design that the zero-translation ADR rejected. Every `/v1/messages` request would cross the lossy type.

---

## Decision

**Chosen option:** Option B.

`/v1/messages` may be translated to Chat Completions when both conditions are true:

*   The target model does not serve the Anthropic dialect natively.
*   The catalog lists `anthropic` for the model through the per-model list below.

### Why this does not repeat the old bugs

| | Zero-translation ADR bugs | This ADR |
|---|---|---|
| Requests that cross the translation | Every `/v1/messages` request | Non-Claude Bedrock models in the per-model list only |
| Position | Middle of the handler path | Bedrock connector, behind `AnthropicConnector` |
| Types | Custom lossy domain type | Anthropic SDK type to OpenAI SDK type |
| Claude models | Broken | Unchanged |

A converter defect affects only the listed models.

### Catalog: per-model list

*   `computeDialects` adds `anthropic` for an exact list of model IDs. The match ignores the region prefix (`global.`, `au.`, and so on), as `stripRegionPrefix` does.
*   The list starts with `zai.glm-5.3`.
*   A model joins the list only after a streaming Claude Code run with tool calls passes on it.
*   Each entry also holds the model's output token limit. The connector caps `max_tokens` to this limit, because Claude Code sends large values. The value for `zai.glm-5.3` is 128K, per its model card.
*   Each entry also holds a default `reasoning_effort`. The connector sets it on every request. The value for `zai.glm-5.3` is `high`. Without it, GLM 5.3 uses `max`, its slowest and most costly setting.
*   Each entry also states whether the model supports Bedrock explicit prompt caching on Chat Completions. The GLM 5.3 model card states explicit caching, a 1024-token minimum and retention of at least 30 minutes. It does not specify the Chat Completions request field for a breakpoint or a maximum write count. The Bedrock prompt caching page documents `prompt_cache_breakpoint` on Chat Completions text parts for GPT-5.6 and later only. A live test must confirm both the field and the write limit for GLM 5.3 before the connector sends breakpoints or drops excess ones. GLM 5.3 implicit caching is supported.

A family rule (`zai.`, `moonshot.`, `minimax.`) was rejected. Tool-calling quality differs per model, and a family rule turns on new models with no test.

### Request mapping

| Anthropic | Chat Completions |
|---|---|
| `system` string or text blocks | One leading `system` message. Each text block becomes one text content part, so a cache breakpoint keeps its position. |
| `cache_control` on a text block (`system`, user) | A Bedrock explicit cache breakpoint on the matching text content part, only if the model entry has a verified breakpoint field. Otherwise dropped. |
| Top-level `cache_control` | An explicit breakpoint, only if the final cacheable block becomes a supported text content part and the model entry has a verified breakpoint field. Otherwise dropped and logged. |
| `cache_control` on a `tool_result` block | An explicit breakpoint on the final text content part of the result, only if the model entry has a verified breakpoint field. Otherwise dropped and logged. |
| `cache_control` on a tool definition | Dropped and logged. No matching text content part exists. |
| Cache breakpoint limits and `ttl` | The connector enforces the write limit only after the live test confirms it for the model. The requested `ttl` is dropped. Bedrock retention applies. |
| Trailing `system`-role messages | Folded into the last user turn by the Amendment 1 fold. The fold moves from `compatible/trailing_system.go` to `convert`, so one implementation exists. The `compatible` connector still calls it on both Messages paths. Listed Bedrock paths call it before conversion. |
| User `text` blocks | `user` message content |
| User `tool_result` blocks | One `tool` message per block, before any remaining user text of that turn. `tool_call_id` is `tool_use_id`. The content keeps `text` subblocks in order. A `tool_reference` subblock renders its `tool_name`. Every other non-text subblock (`image`, `document`, `search_result`, `browser_state`, other) becomes a short text notice that names the omitted type. The loss is logged and the block order is kept. `is_error: true` puts an `Error: ` prefix on the resulting content. |
| User `image` blocks | `image_url` content parts. The upstream rejects them if the model has no vision. |
| Assistant `text` | `content` |
| Assistant `tool_use` | `tool_calls[]`, with `arguments` set to the JSON of `input` |
| Assistant `thinking`, `redacted_thinking` | Dropped |
| Assistant `server_tool_use` and `*_tool_result` blocks (for example tool search) | Dropped |
| Custom tools | `function` tools. `input_schema` becomes `parameters`. |
| Typed client tools (`bash_*`, `text_editor_*`, `computer_*`) | Dropped and logged |
| Server tools (`web_search_*`, `web_fetch_*`, `code_execution_*`, `advisor_*`) | Dropped and logged |
| Tool search tools (`tool_search_tool_regex`, `tool_search_tool_bm25`) | Dropped and logged |
| Custom tools with `defer_loading: true` | Sent as normal `function` tools. The count of deferred tools is logged. |
| `strict` on custom tools | Dropped and logged |
| `tool_choice` | `auto` to `auto`, `any` to `required`, `tool` to a named function, `none` to `none`. A choice that names a dropped tool is ignored. `disable_parallel_tool_use` becomes `parallel_tool_calls: false`. |
| `max_tokens` | `max_tokens`, capped to the model's limit |
| `temperature`, `top_p`, `stop_sequences` | `temperature`, `top_p`, `stop` |
| `top_k`, `metadata`, `thinking`, `service_tier` | Dropped. The connector sets `reasoning_effort` to the model entry's default. |
| `output_config.format` | Dropped and logged |

The rule for a dropped tool in `tool_choice` matches the Responses path in `convert/responses_chatcompletions.go`.

### Response mapping

*   `message.content` becomes a `text` block.
*   Each `tool_calls[i]` becomes a `tool_use` block. Empty `arguments` become `{}`. Invalid JSON becomes `{}` and logs a warning. An empty tool-call ID becomes `toolu_<n>`. This fallback is separate from the message ID below.
*   `finish_reason`: `stop` to `end_turn`, `length` to `max_tokens`, `tool_calls` and `function_call` to `tool_use`, `content_filter` to `refusal`.
*   `stop_sequence` is always null. Chat Completions does not report which sequence matched.
*   Usage: `cached_tokens` becomes `cache_read_input_tokens`. `completion_tokens` becomes `output_tokens`. If Bedrock reports cache-write tokens, they become `cache_creation_input_tokens`, and `input_tokens` is `prompt_tokens` minus cache writes and cached reads. If Bedrock reports no cache writes, `input_tokens` is `prompt_tokens` minus `cached_tokens`. It includes any cache writes, so the estimated cost can differ from Bedrock billing. The Bedrock response field for GLM 5.3 cache-write tokens is not known.
*   Cache-write field check: Bedrock documents `cache_write_tokens` for [GPT-5.6 and later](https://docs.aws.amazon.com/bedrock/latest/userguide/prompt-caching.html#prompt-caching-openai-56-usage). The GLM 5.3 live test checks whether its Chat Completions response reports this field and where. The GPT response shape is not assumed for GLM 5.3.
*   `reasoning_content` is ignored.
*   Response identity: `id` is the completion `id`, or a generated stable per-response ID if it is empty. `type` is `message`. `role` is `assistant`. `model` is the resolved Bedrock model ID. Streaming `message_start` carries the same values.

### Streaming

*   The bridge sets `stream_options.include_usage` to true. The first chunk produces `message_start`, with the response identity values from Response mapping.
*   Content deltas open a `text` block and produce `text_delta` events.
*   A tool-call delta with a new `index` closes the open block and opens a `tool_use` block. Argument fragments become `input_json_delta` events.
*   On `finish_reason`, the open block closes. `message_delta` and `message_stop` wait for the usage chunk, or for the end of the stream if no usage chunk arrives. `message_delta` carries the returned token counts and comes before `message_stop`. If no usage chunk arrives, the bridge logs a warning and sends the counts it received. A run with no counts records zero usage and zero estimated cost. The GLM 5.3 acceptance run confirms the usage chunk.
*   A response with no content produces one empty `text` block.
*   A mid-stream error becomes `AnthropicStreamEvent{Err}`. It is suppressed after the terminal event.

### Feature survival

| Feature | Result |
|---|---|
| Tool use, tool results, parallel tool calls | Survive |
| Multi-block system prompts | Survive, as separate text content parts. |
| Streaming event shape, stop reasons | Survive. `stop_sequence` is not reported. |
| Usage, cache-read tokens | Survive. Cache-write tokens map to `cache_creation_input_tokens` only if Bedrock reports them. Otherwise they are counted in `input_tokens`. |
| Prompt caching | GLM 5.3 implicit caching is supported. Explicit breakpoints map only after the live field test confirms the field. Other listed models map explicit breakpoints only with verified support. Otherwise `cache_control` is dropped and implicit caching may apply. The requested `ttl` is dropped. |
| Tool results | Text and `tool_reference` names survive. Non-text contents (images, documents, search results, browser state) are lost and replaced by a text notice. `is_error` becomes an `Error: ` prefix. The flag is lost. |
| Extended thinking | Lost in v1. The client cannot set the reasoning effort or see the reasoning. The model reasons at the default effort from its entry. |
| Typed client tools | Lost in v1 |
| Server tools, `top_k` | Lost |
| Images | Passed through. They depend on model vision support. |
| Mid-conversation tool changes (`tool_addition`, `tool_removal`) | Lost in v1. The trailing system fold keeps text blocks only. |
| Tool search | Lost in v1. Every deferred tool is sent on every turn, which adds tokens to each request. |
| Strict tool schemas (`strict`) | Lost. The model can return arguments that do not match the schema. |
| JSON answer format (`output_config.format`) | Lost. The caller gets free text. |

### Server tools

The GLM 5.3 model card marks server-side tool use as not supported. Bedrock documents Claude tool search through `InvokeModel` and `InvokeModelWithResponseStream`, not through the Messages endpoint. The chosen Chat Completions path has no Anthropic server-tool equivalent. The converter drops server tools and logs the drop. Claude Code still sends them, because it treats the gateway as the Anthropic API.

---

## Consequences

### Positive

*   Claude Code agents can use GLM 5.3 on Bedrock, and repository code stays in the AWS account.
*   Claude requests are unchanged.
*   The converter is provider-agnostic. The `compatible`, Vertex AI and Azure connectors can reuse it.

### Negative

*   The upstream request for a listed model differs from the client request. The losses are listed under Feature survival.
*   A dropped server tool fails silently for the user. Only the gateway log shows the drop.
*   Each new model needs a test run and a list entry.

### Neutral

*   The Amendment 1 fold moves to `convert`. The `compatible` connector calls it from there, so its behaviour does not change.

---

## Future work

*   **Thinking (v2):** map the `thinking` budget to `reasoning_effort`, and map `reasoning_content` to a `thinking` block. First check that Claude Code accepts a `thinking` block with no signature.
*   **Typed client tools (v2):** add the published Anthropic schema for each tool version as a `function` tool, and map the call back to `tool_use`.
*   **Mid-conversation tool changes (v2):** apply `tool_addition` and `tool_removal` blocks from `system`-role messages to the translated `tools` list. Check first whether Claude Code sends these blocks.

---

## Implementation

1.  This ADR. Add a pointer from the zero-translation ADR non-goal "Cross-dialect failover" to this ADR. The pointer states that connector-local translation does not enable failover. Update the Amendment 1 Bedrock statement and its Last Updated line (2026-11-17).
2.  `providers/convert/anthropic_chatcompletions.go` and tests, including the trailing system fold moved from `compatible/trailing_system.go`. No behaviour change.
3.  `providers/bridge/chat_completions_messages.go`, the Bedrock connector change, the per-model list and the acceptance run.

In `bedrock/anthropic.go`, `AnthropicMessages` and `AnthropicMessagesStream` switch on `dispatchBackend`:

*   `backendAnthropic`: the existing path.
*   `backendOpenAICompat`: the shared fold, then the bridge over the OpenAI-compatible client.
*   `backendMantle`: the shared fold, then the bridge over the Mantle client.
*   `backendMantleOpenAIV1`, `backendTitanEmbed`, `backendCohereEmbed` and an unnamed backend: `MODEL_NOT_SUPPORTED` from both methods.

---

## Validation

*   A Claude Code agent on a virtual model routed to `bedrock:aus` `global.zai.glm-5.3` completes a streaming run that calls tools.
*   `convert` table tests cover tool use, tool results, system blocks and stop reasons in both directions.
*   Stream tests cover text only, one fragmented tool call, parallel tool calls, a whole tool call in one chunk, text then a tool call, a `length` finish, no usage chunk, and a mid-stream error.
*   The streamed events, put through `anthropic.Message.Accumulate`, equal the non-streaming conversion of the same completion.
*   A Claude model still takes the zero-translation path. A model not in the list does not get `anthropic`. A `backendMantleOpenAIV1`, `backendTitanEmbed`, `backendCohereEmbed` or unnamed-backend model still returns `MODEL_NOT_SUPPORTED` from both methods.

---

## Related Decisions

*   [Anthropic zero-translation path](2026-04-01-anthropic-zero-translation-path.md): its zero-translation rule remains in force for Claude. This ADR permits connector-local translation for listed non-Claude Bedrock models. Cross-dialect failover stays out of scope.
*   [Provider model catalog and alias routing](2026-04-01-provider-model-catalog-and-alias-routing.md)
*   [CRD inference providers](2026-10-01-crd-inference-providers.md)

## References

*   [Bedrock Claude tool use and tool search](https://docs.aws.amazon.com/bedrock/latest/userguide/model-parameters-anthropic-claude-messages-tool-use.html)
*   [Bedrock model API compatibility](https://docs.aws.amazon.com/bedrock/latest/userguide/models-api-compatibility.html)
*   [Bedrock GLM 5.3 model card](https://docs.aws.amazon.com/bedrock/latest/userguide/model-card-zai-glm-5-3.html)
