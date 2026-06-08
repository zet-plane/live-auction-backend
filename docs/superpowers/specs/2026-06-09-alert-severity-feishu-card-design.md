# Alert Severity Feishu Card Design

## Context

Online alerts currently reach Feishu, but the received messages do not make severity obvious enough for operators. Prometheus alert rules already attach a `severity` label, and the repository already contains a custom `feishu-webhook` adapter that can parse Alertmanager payloads and build Feishu interactive cards.

This design keeps the scope narrow: make Feishu alert cards visibly distinguish alert levels. It does not introduce separate on-call groups, escalation policies, or different repeat intervals in this iteration.

## Goals

- Show alert severity clearly in Feishu card title and fields.
- Map severity to stable visual treatment:
  - `critical`: red card, Chinese label `严重告警`.
  - `warning`: yellow card, Chinese label `警告告警`.
  - `info`: blue card, Chinese label `提示告警`.
  - unknown severity: grey card, Chinese label `未分级告警`.
- Keep resolved messages green while still showing the original severity label.
- Keep Alertmanager routing simple and continue sending resolved notifications.
- Avoid committing Feishu webhook URLs, signing secrets, or other reusable secrets.

## Non-Goals

- Do not add different Feishu groups or receivers per severity.
- Do not change alert thresholds or decide new business alert rules.
- Do not add on-call escalation or duty scheduling.
- Do not alter online Kubernetes resources directly as part of the code change.

## Current State

`deploy/k8s/14-alert-rules.yaml` already labels alerts as `critical` or `warning`. `deploy/k8s/13-alertmanager.yaml` sends alerts to PrometheusAlert with the default `prometheus-fs` template. That template is not controlled in this repository, so the visible severity format is hard to guarantee.

`deploy/k8s/15-feishu-webhook.yaml` contains a custom Python webhook adapter. It already reads `labels.severity`, chooses a card color, includes the severity field, and sends signed Feishu interactive cards. It should become the controlled path for visible severity formatting.

## Design

Alertmanager should send Feishu notifications to the custom `feishu-webhook` service instead of PrometheusAlert:

```text
Prometheus alert rule
  -> Prometheus
  -> Alertmanager
  -> feishu-webhook
  -> Feishu custom bot
```

The `feishu-webhook` card builder should normalize severity before rendering:

| Raw severity | Display label | Firing color |
| --- | --- | --- |
| `critical` | `严重告警` | red |
| `warning` | `警告告警` | yellow |
| `info` | `提示告警` | blue |
| other or missing | `未分级告警` | grey |

Firing card titles should include the display label and alert name, for example:

```text
严重告警 · HighErrorRate
警告告警 · HighP95Latency
```

Resolved card titles should use green visual treatment but keep the severity display label in the card body:

```text
告警恢复 · HighErrorRate
```

The card body should continue to include summary, description, trigger time, and recovery time when present.

## Secret Handling

Kubernetes manifests committed to the repository must not include real Feishu webhook URLs or signing secrets. The committed files should either:

- use a Secret name whose values are provisioned outside Git, or
- keep only a `.example` manifest with placeholders.

The implementation should not print secret values in tests or verification output.

## Testing

Use local, offline checks only:

- Parse the Kubernetes YAML enough to confirm Alertmanager points to `feishu-webhook`.
- Unit-check or script-check card rendering for `critical`, `warning`, `info`, and missing severity without sending network requests.
- Confirm committed manifests do not contain Feishu webhook URLs or signing secret values.

Online verification, if requested later, should follow `skills/live-auction-online-ops/SKILL.md` and use read-only checks first.
