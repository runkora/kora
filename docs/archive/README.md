# Archive

Superseded, retired, or point-in-time documents — preserved for history, **not** current guidance. If something here contradicts the live docs, trust the live docs.

## What's here

| File | What it was |
|------|-------------|
| [`prd-model-gateway.md`](prd-model-gateway.md) | The original "Model Gateway" PRD. Retired and superseded by the [Inference Gateway PRD](../prd/inference-gateway.md). |
| [`policy-enforcement-redesign.md`](policy-enforcement-redesign.md) | Working design doc for the policy redesign; codified into the policy ADRs (see the [ADR index](../adr/README.md#policy--authorization)). |
| [`feedback-inference-analytics.md`](feedback-inference-analytics.md) | One-time review notes on the inference-analytics design; feedback was incorporated. |
| [`agents-merge-audit.md`](agents-merge-audit.md) | Branch merge-readiness audit for the agents control plane (PR #53/#61); the work is shipped. |
| [`2026-05-11-agents-*.md`](.) | Four dated implementation plans for the frontend agents UI/BFF/run-chat work; all shipped (PR #61). |
| [`agent-chat-ui-spec.md`](agent-chat-ui-spec.md), [`agent-control-plane-design.md`](agent-control-plane-design.md), [`agents-ui-scratch.md`](agents-ui-scratch.md), [`config-layering.md`](config-layering.md) | Scratch design notes; their content was codified in the corresponding PRDs/ADRs. |
| [`agent-dashboard.html`](agent-dashboard.html), [`agent-settings-v2.html`](agent-settings-v2.html), [`terra-agent-chat-v2.html`](terra-agent-chat-v2.html) | Static HTML mockups; superseded by the shipped UI under `frontend/src/app/(dashboard)/`. |

## Why keep them?

A few ADRs and PRDs reference these documents in prose ("the retired Model Gateway PRD…", "the original design at…"). Keeping them in `archive/` preserves traceability without polluting the discoverable docs tree. To find current information, start at [`../README.md`](../README.md).
