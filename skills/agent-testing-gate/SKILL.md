---
name: agent-testing-gate
description: Project-local testing gate for live-auction-backend. Use when an agent plans, chooses, or runs tests in this repo, especially interface, integration, end-to-end, concurrency, state-consistency, performance, online, DB/Redis/WebSocket/HTTP, or docs/agent-testing based tests.
---

# Agent Testing Gate

This skill is project-local to `live-auction-backend`. Do not copy or generalize it to other repositories.

## Trigger

Use this skill whenever the task involves testing in this repo:

- choosing what tests to run
- writing a test plan
- running tests beyond local unit tests
- diagnosing failures that may require HTTP, WebSocket, MySQL, Redis, online, or online-equivalent dependencies
- using anything under `docs/agent-testing/`

## Workflow

1. Classify the test type before running commands.
2. For local code unit tests, use normal repo commands such as `rtk go test ./...`; they must not connect to MySQL, Redis, HTTP services, WebSocket, or external systems.
3. For any interface, integration, end-to-end, concurrency, state-consistency, performance, online, or dependency-backed test, read `docs/agent-testing/README.md` first.
4. Follow the README's progressive-disclosure route. Do not bulk-read `docs/agent-testing/`.
5. Before executing a test according to `docs/agent-testing/`, ask the human for explicit approval. The request must state:
   - which `docs/agent-testing/` route will be used
   - whether real or online-equivalent dependencies may be touched
   - what data scope or batch prefix will be used
   - what evidence/report will be produced
6. Do not proceed with that docs-driven test until the human approves it in the conversation.

## Approval Boundary

Approval is required every time a test will be executed according to `docs/agent-testing/`, even if a previous test was approved.

Reading `docs/agent-testing/README.md` to decide the route does not require approval. Executing the routed test does.

## Safety

- Never print or write secrets, reusable tokens, database DSNs, Redis credentials, kubeconfig contents, proxy credentials, or online addresses into reports.
- Operate only on data created for the current approved test batch.
- If the required business rule, pass criteria, concurrency semantics, or final invariant is missing, ask the human before writing or running the plan.
