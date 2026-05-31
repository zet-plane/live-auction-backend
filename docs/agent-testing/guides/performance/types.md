# 性能压测类型定义

本指南定义性能压测专属类型。普通 agent-testing 的“真实依赖”字段不足以表达性能压测的环境、压测源、阶段模型、监控、判停和证据要求，因此正式性能压测计划必须使用本文件的类型。

## PerformanceTestPlan

```text
PerformanceTestPlan:
  target:
  environment: PerformanceEnvironment
  load_source: LoadSource
  load_model: LoadModel
  thresholds: Thresholds
  stop_conditions: StopCondition[]
  observability: ObservabilityPlan
  business_reconcile: BusinessReconcilePlan
  subagents: SubagentExecutionPlan
  evidence: PerformanceEvidence
  cleanup:
  approval:
```

## PerformanceEnvironment

```text
PerformanceEnvironment:
  kind: local_smoke | single_source_online | staging_capacity | production_guarded
  service_scope:
  deploy_target:
  entrypoint:
  k8s_namespace:
  app_workload:
  dependency_scope:
  observability_stack:
  risk_window:
  rollback_contact:
```

环境类型含义：

| kind | 含义 | 最低要求 | 允许结论 |
| --- | --- | --- | --- |
| `local_smoke` | 本地或开发依赖的小流量脚本验证 | 本地服务可访问，runner 可运行 | 只能证明脚本、数据和对账逻辑可运行 |
| `single_source_online` | 单个压测源打线上或线上等价环境 | 记录压测源规格和瓶颈风险 | 可发现粗略瓶颈，不能单独证明正式容量上限 |
| `staging_capacity` | 准生产容量验证 | 独立压测源、完整监控、业务对账 | 可作为准生产容量结论 |
| `production_guarded` | 线上受控压测 | 批准窗口、人工旁路监控、停止条件、清理策略 | 可形成当前线上环境的受控容量结论 |

## LoadSource

```text
LoadSource:
  kind: local_machine | remote_machine | load_platform | k8s_job
  count:
  cpu:
  memory:
  network_location:
  outbound_identity:
  tool:
  max_supported_qps:
  known_limit:
```

压测源信息必须足以判断瓶颈是否出在压测端。不能说明压测源规格和网络位置时，结论不得写成正式容量上限。

## LoadModel

```text
LoadModel:
  stages:
    - name: smoke | step_load | peak_hold | soak
      target_qps:
      target_concurrency:
      duration:
      ramp:
      request_mix:
```

线上压测必须包含 smoke。`production_guarded` 不得跳过 step load 直接进入 peak hold。

## Thresholds 和 StopCondition

```text
Thresholds:
  p95:
  p99:
  error_rate:
  timeout_rate:
  business_failure_rate:
  cpu:
  memory:
  mysql:
  redis:
  websocket:

StopCondition:
  metric:
  threshold:
  duration:
  action: stop_load | hold_stage | abort_test | ask_human
```

停止条件优先级高于压测目标。触发停止条件后，agent 不得继续加压。

## ObservabilityPlan

```text
ObservabilityPlan:
  prometheus:
  grafana:
  logs:
  traces:
  kubectl:
  mysql:
  redis:
  sample_interval:
  evidence_format:
```

缺少监控计划时，线上压测只能停留在计划阶段。

## BusinessReconcilePlan

```text
BusinessReconcilePlan:
  sample_scope:
  http_checks:
  mysql_checks:
  redis_checks:
  websocket_checks:
  invariant:
  failure_action:
```

性能压测结论必须包含业务状态对账。只看吞吐、延迟和状态码，不得写“通过”。

## SubagentExecutionPlan

```text
SubagentExecutionPlan:
  mode: main_agent_only | main_agent_with_subagents
  roles:
    - preflight
    - monitor
    - load
    - recorder
    - cleanup
  shared_batch_id:
  role_boundaries:
  stop_signal:
```

subagent 编排不是业务测试计划的一部分。计划批准后，主 agent 可以使用该类型约束执行器。

## PerformanceEvidence

```text
PerformanceEvidence:
  plan_path:
  approval_record:
  runner_path:
  preflight:
  stage_results:
  observability:
  logs:
  business_reconcile:
  stop_events:
  cleanup:
```

## PerformanceReport

```text
PerformanceReport:
  environment_kind:
  conclusion: passed | failed | stopped | inconclusive | skipped
  capacity:
  bottleneck:
  degradation:
  risks:
  next_actions:
```

结论含义：

| conclusion | 含义 |
| --- | --- |
| `passed` | 达到目标压力，延迟、错误率、资源指标满足阈值；业务对账通过；清理完成 |
| `failed` | 延迟、错误率、资源瓶颈或业务状态任一核心指标破阈 |
| `stopped` | 触发停止条件或人工要求停止；必须记录触发点和已完成阶段 |
| `inconclusive` | 缺少批准记录、监控、压测源信息、runner 输出、业务对账或清理结果 |
| `skipped` | 环境未授权、数据不足、目标契约缺失或风险超出计划边界 |
