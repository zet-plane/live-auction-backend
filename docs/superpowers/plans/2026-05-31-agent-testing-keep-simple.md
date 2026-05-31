# Agent Testing Keep Simple Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Simplify `docs/agent-testing/` by introducing a shared protocol template, shortening the main concurrency/performance guide names, and reducing duplicated execution rules.

**Architecture:** Keep the existing directory shape. Put reusable protocol content in `docs/agent-testing/templates/protocol.md`; keep `docs/agent-testing/guides/runner.md` as the execution router; keep specialized guides focused on their unique rules.

**Tech Stack:** Markdown documentation, existing `docs/agent-testing/` contracts, `rtk`-prefixed shell commands, git-aware file moves.

---

## Current Worktree Warning

The `docs/agent-testing/` tree already has uncommitted changes. Workers must not revert them. Implement against the current filesystem state and only edit the files assigned to each task.

Expected current naming state before implementation:

```text
docs/agent-testing/guides/concurrency-consistency.md exists
docs/agent-testing/guides/performance-load.md exists
docs/agent-testing/guides/concurrency.md may be deleted in git status
```

The final desired naming state:

```text
docs/agent-testing/guides/concurrency.md
docs/agent-testing/guides/performance.md
docs/agent-testing/templates/protocol.md
```

## File Structure

| File | Responsibility |
| --- | --- |
| `docs/agent-testing/templates/protocol.md` | Shared protocol template: progressive reading, plan fields, dependency authorization, data isolation, evidence, reporting, cleanup, sensitive data, failure output, escalation. |
| `docs/agent-testing/README.md` | Thin map: first-level routing, directory tree, minimal hard rules. |
| `docs/agent-testing/guides/runner.md` | Execution router: task classification and test-type table; references protocol for common rules. |
| `docs/agent-testing/guides/concurrency.md` | Short-name concurrency guide, keeping concurrency-specific approval and execution rules. |
| `docs/agent-testing/guides/performance.md` | Short-name performance guide, keeping performance-specific model, metrics, and thresholds. |
| `docs/agent-testing/guides/subagent.md` | Subagent-specific guide; references protocol for common evidence/report/cleanup/sensitive-data rules. |
| `docs/agent-testing/guides/environment.md` | Environment-specific guide; references protocol for common data naming/cleanup/sensitive-data rules where useful. |
| `docs/agent-testing/guides/go-runner.md` | Runner-specific guide; references protocol for common evidence/report/cleanup/sensitive-data rules where useful. |
| `docs/agent-testing/reports/README.md` | Report template source; references protocol for common evidence/cleanup/sensitive-data rules. |
| `docs/agent-testing/templates/module.md` | Module contract template; references protocol for common dependency/evidence/cleanup rules. |

Do not edit `docs/agent-testing/modules/*.md`, `docs/agent-testing/flows/*.md`, existing report files, or `docs/agent-testing/templates/runner.go`.

## Task 1: Create Protocol Template

**Files:**
- Create: `docs/agent-testing/templates/protocol.md`

- [ ] **Step 1: Add the protocol template**

Use `apply_patch` to create `docs/agent-testing/templates/protocol.md` with this content:

````markdown
# Agent Testing 通用协议

本模板定义所有 agent-testing 任务共同遵守的通用协议。它不定义具体模块业务规则，不替代模块或流程契约，也不展开并发、性能、subagent 等专项执行方法。

## 适用范围

本协议适用于：

- 本地单元测试。
- 接口契约测试。
- 模块集成测试。
- 全流程测试。
- 状态一致性测试。
- 并发一致性测试。
- 性能压测。
- WebSocket 测试。
- subagent 编排测试。
- 测试报告沉淀。

专项 guide 可以追加字段和步骤，但不能放宽本协议中的安全边界。

## 渐进式读取

默认读取顺序：

```text
docs/agent-testing/README.md
docs/agent-testing/templates/protocol.md
docs/agent-testing/guides/runner.md
目标 modules/<module>.md 或 flows/<flow>.md
```

只在需要时读取专项文档：

- 环境准备、连接 DB/Redis、启动服务或创建测试数据：读 `guides/environment.md`。
- 使用 Go runner 采集结构化证据：读 `guides/go-runner.md` 和 `templates/runner.go`。
- 设计或执行并发一致性测试：读 `guides/concurrency.md`。
- 设计或执行性能压测：读 `guides/performance.md`。
- 使用 subagent：读 `guides/subagent.md`。
- 写入或校验报告：读 `reports/README.md`。
- 生成模块契约：读 `guides/module-generator.md` 和 `templates/module.md`。

禁止一次性读取整个 `docs/agent-testing/` 目录。

## 测试计划字段

执行测试前，agent 必须输出测试计划。通用字段：

```text
测试目标：
读取文档：
测试范围：
禁止范围：
测试类型：
测试数据：
依赖策略：
执行步骤：
验证方式：
预计输出：
```

专项 guide 可以追加字段：

- `guides/subagent.md`：追加主 agent 职责、subagent 分工、可读取项目上下文、真实依赖授权和并行数据隔离策略。
- `guides/concurrency.md`：追加场景名称、竞争对象、并发请求、预期成功、预期失败、最终不变量、审核状态和执行许可。
- `guides/performance.md`：追加压测模型、目标阈值、压测源、监控指标、停止条件和业务抽样对账。

如果测试计划超出目标契约边界，必须先征得用户确认。

## 依赖授权

本地单元测试必须隔离数据库、Redis、HTTP 服务、WebSocket 和外部系统，使用 mock、fake、进程内数据、固定时间和固定 ID。

Agent 执行接口契约、模块集成、全流程、并发一致性、性能压测和状态一致性测试时，可以连接测试库、线上等价真实依赖或用户明确授权的真实依赖。

连接真实依赖前必须满足：

- 目标契约或测试计划明确允许真实依赖。
- 测试数据边界明确。
- 清理策略明确。
- 地址、凭据、token 不写入报告。

如果计划没有注明是否连接真实依赖，agent 只能生成计划或阻塞项，不得自行连接。

## 测试数据隔离

所有真实依赖测试必须使用可识别测试批次、名称前缀、幂等 key 前缀、Redis key 前缀或明确实体 ID 集合。

并行测试或 subagent 并行执行时，每个并行任务必须有唯一子批次或互不重叠的数据范围。

禁止：

- 使用非本批次数据做破坏性操作。
- 按模块、状态或时间范围批量修改无法识别归属的数据。
- 清空数据库或 Redis。
- 复用真实用户手机号、支付信息或可复用 token。

## 证据要求

每个测试结论都必须有证据支撑。可接受证据包括：

- 测试命令和退出码。
- HTTP 请求和响应摘要。
- WebSocket 消息摘要。
- MySQL 查询结果摘要。
- Redis 查询结果摘要。
- Go 测试输出摘要。
- Go runner CASE / SUMMARY / CLEANUP 输出。
- 关键日志片段、日志文件路径或检索方式。

证据必须能对应到验证点和结果。不要只写“通过”或“失败”。

长日志只记录关键片段、路径或检索方式。

## 报告沉淀

执行测试后，应按 `docs/agent-testing/reports/README.md` 沉淀报告。

报告必须至少说明：

- 测试目标。
- 读取文档。
- 测试范围和跳过范围。
- 依赖策略。
- 测试数据和批次。
- 执行步骤。
- 验证证据。
- 通过项、失败项和跳过项。
- 风险和建议。
- 建议沉淀的回归测试。
- 已知缺口。
- 测试数据清理结果。

使用 subagent 时，报告必须区分子 agent 原始结论和主 agent 复核结论。

## 清理规则

测试创建数据时，必须记录：

```text
测试批次 ID：
创建的数据：
清理方式：
清理结果：
未清理原因：
```

只能清理本次批次、前缀或明确实体 ID 集合内的数据。

禁止执行：

- `DROP DATABASE`
- `DROP TABLE`
- `TRUNCATE`
- `FLUSHALL`
- `FLUSHDB`

如果多个 subagent 并行执行，清理记录必须按 subagent 区分子批次、前缀或实体 ID 集合。

## 敏感信息规则

报告和文档中禁止写入：

- 数据库密码。
- Redis 密码。
- Apifox token。
- 生产环境地址。
- 线上数据库地址。
- 线上 Redis 地址。
- 真实用户手机号。
- 真实支付信息。
- 任何可复用认证 token。

认证信息只能写成：

```text
认证方式：使用测试环境 token，具体值已省略。
```

## 失败输出

测试失败时，必须输出：

```text
失败场景：
复现步骤：
期望结果：
实际结果：
相关证据：
可能原因：
影响范围：
建议修复点：
建议新增的回归测试：
```

如果是不变量或一致性失败，还必须说明违反位置、相关数据源和最终状态。

## 必须停止并询问的情况

出现以下情况时，agent 必须停止并询问用户或主 agent：

- 目标模块或流程不明确。
- 业务规则缺失。
- 通过标准缺失。
- 关键状态流转没有定义。
- 并发优先级没有定义。
- 模块文档和流程文档冲突。
- 测试依赖不明确，且会影响结论。
- 计划没有注明真实依赖授权。
- 测试数据边界不清楚。
- 清理策略可能影响非本批次数据。
- 发现文档外风险，需要扩大测试范围。

## 专项附加规则入口

专项规则只写差异：

- 并发一致性测试：见 `docs/agent-testing/guides/concurrency.md`。
- 性能压测：见 `docs/agent-testing/guides/performance.md`。
- subagent 编排：见 `docs/agent-testing/guides/subagent.md`。
- Go runner 证据采集：见 `docs/agent-testing/guides/go-runner.md`。
- 环境准备：见 `docs/agent-testing/guides/environment.md`。
- 报告模板：见 `docs/agent-testing/reports/README.md`。
````

- [ ] **Step 2: Verify protocol template**

Run:

```bash
rtk rg -n "通用协议|测试计划字段|依赖授权|测试数据隔离|证据要求|清理规则|敏感信息规则|必须停止并询问" docs/agent-testing/templates/protocol.md
rtk rg -n "TB[D]|TO[D]O|待[定]|FIXM[E]|placeholde[r]" docs/agent-testing/templates/protocol.md
```

Expected: first command finds all major sections; second command exits with no matches.

## Task 2: Rename Short Capability Guides

**Files:**
- Rename: `docs/agent-testing/guides/concurrency-consistency.md` -> `docs/agent-testing/guides/concurrency.md`
- Rename: `docs/agent-testing/guides/performance-load.md` -> `docs/agent-testing/guides/performance.md`

- [ ] **Step 1: Move current files to short names**

Use `apply_patch` move hunks when possible. If a source is untracked and `apply_patch` cannot move it, use `rtk mv` only for the two assigned files.

Desired result:

```text
docs/agent-testing/guides/concurrency.md
docs/agent-testing/guides/performance.md
```

- [ ] **Step 2: Update titles only if needed**

Ensure titles remain clear:

```markdown
# 并发一致性测试指南
# 性能压测指南
```

Do not rewrite the body in this task.

- [ ] **Step 3: Verify short guide names**

Run:

```bash
rtk ls docs/agent-testing/guides
rtk sed -n '1,12p' docs/agent-testing/guides/concurrency.md
rtk sed -n '1,12p' docs/agent-testing/guides/performance.md
```

Expected: short files exist; old long-name files are absent from the filesystem.

## Task 3: Thin README

**Files:**
- Modify: `docs/agent-testing/README.md`

- [ ] **Step 1: Replace quick-entry table**

Keep the heading and intro. Replace the quick-entry rows with:

```markdown
| 任务 | 下一步读取 |
| --- | --- |
| 所有测试执行任务 | `templates/protocol.md` -> `guides/runner.md` -> 目标 `modules/<module>.md` 或 `flows/<flow>.md` |
| 并发一致性测试 | `templates/protocol.md` -> `guides/runner.md` -> `guides/concurrency.md` -> `guides/go-runner.md` -> 目标契约 |
| 性能压测 | `templates/protocol.md` -> `guides/runner.md` -> `guides/performance.md` -> `guides/environment.md` -> 目标契约 |
| subagent 编排 | `templates/protocol.md` -> `guides/runner.md` -> `guides/subagent.md` -> 目标契约 |
| 环境准备、连接 DB/Redis、启动服务、创建测试数据 | `templates/protocol.md` -> `guides/environment.md` |
| 使用 Go runner 采集结构化证据 | `templates/protocol.md` -> `guides/go-runner.md` |
| 生成或补充模块测试文档 | `guides/module-generator.md` -> `templates/module.md` |
| 写入或补充测试报告 | `templates/protocol.md` -> `reports/README.md` |
```

- [ ] **Step 2: Update directory tree**

Directory tree must show:

```text
├── guides/
│   ├── runner.md
│   ├── environment.md
│   ├── concurrency.md
│   ├── performance.md
│   ├── go-runner.md
│   ├── subagent.md
│   └── module-generator.md
├── templates/
│   ├── protocol.md
│   ├── module.md
│   └── runner.go
```

- [ ] **Step 3: Replace progressive-reading rules**

Use this shorter rule list:

```markdown
- 不要一次性读取整个目录。
- 先读 `README.md`，再读 `templates/protocol.md`。
- 普通执行任务再读 `guides/runner.md` 和目标契约。
- 专项任务只读取对应专项 guide：并发读 `guides/concurrency.md`，性能读 `guides/performance.md`，subagent 读 `guides/subagent.md`。
- 只有准备环境时才读 `guides/environment.md`。
- 只有编写 Go runner 时才读 `guides/go-runner.md` 和 `templates/runner.go`。
- 只有生成模块契约时才读 `guides/module-generator.md` 和 `templates/module.md`。
- 只有写报告时才读 `reports/README.md`。
```

- [ ] **Step 4: Thin hard rules**

Keep only these hard rules in README:

```markdown
- 本地单元测试不得直连 MySQL、Redis、HTTP 服务、WebSocket 或外部系统。
- 真实依赖测试只能操作本次测试批次、前缀或明确实体 ID 集合内的数据。
- 每个测试结论都必须有证据。
- 目标文档缺少关键业务规则时，agent 必须先询问用户。
- 并发一致性测试必须先按 `guides/concurrency.md` 生成并批准计划。
- subagent 输出只是中间产物，最终结论必须由主 agent 复核。
- 报告不得写入地址、凭据、密码、真实 token 或真实用户敏感信息。
```

- [ ] **Step 5: Verify README**

Run:

```bash
rtk rg -n "templates/protocol.md|guides/concurrency.md|guides/performance.md|concurrency-consistency|performance-load" docs/agent-testing/README.md
```

Expected: references to `templates/protocol.md`, `guides/concurrency.md`, and `guides/performance.md`; no old long paths.

## Task 4: Thin Runner

**Files:**
- Modify: `docs/agent-testing/guides/runner.md`

- [ ] **Step 1: Make runner reference protocol**

Near the top, after the intro, add:

```markdown
通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。本指南只负责执行路由和测试类型选择。
```

- [ ] **Step 2: Replace execution-entry paths**

Update all old paths:

```text
docs/agent-testing/guides/concurrency-consistency.md -> docs/agent-testing/guides/concurrency.md
docs/agent-testing/guides/performance-load.md -> docs/agent-testing/guides/performance.md
```

Ensure normal execution mentions reading `docs/agent-testing/templates/protocol.md` before `runner.md`.

- [ ] **Step 3: Replace long dependency strategy sections with a short matrix**

Keep the current section headings, but make `### 4. 选择依赖策略` shorter by replacing repeated test-type prose with a table:

```markdown
### 4. 选择测试类型和专项指南

通用依赖授权和数据隔离规则见 `docs/agent-testing/templates/protocol.md`。

| 测试类型 | 下一步读取 | 重点 |
| --- | --- | --- |
| 单元测试 | 目标代码和单测 | fake/mock，禁止真实依赖 |
| 接口契约测试 | 目标契约 + `guides/go-runner.md` | HTTP 请求/响应和 DTO 对齐 |
| 模块集成测试 | 目标契约 + `guides/go-runner.md` | 真实 DAO / Service / DB / Redis |
| 全流程测试 | `flows/<flow>.md` + 关联模块契约 | 跨模块闭环和状态一致性 |
| 并发一致性测试 | `guides/concurrency.md` + `guides/go-runner.md` | 竞争窗口、原子性和最终一致性 |
| 性能压测 | `guides/performance.md` + `guides/environment.md` | 容量、延迟、错误率和资源瓶颈 |
| WebSocket 测试 | 目标契约 + `guides/go-runner.md` | 真实连接和消息证据 |
| subagent 编排 | `guides/subagent.md` | 分工、授权、隔离和主 agent 复核 |
```

Remove duplicated long subsections only if they are fully covered by `templates/protocol.md` or a specialized guide. Do not remove unique Apifox alignment guidance unless it is preserved elsewhere in runner.

- [ ] **Step 4: Verify runner**

Run:

```bash
rtk rg -n "templates/protocol.md|guides/concurrency.md|guides/performance.md|concurrency-consistency|performance-load|选择测试类型和专项指南|Apifox" docs/agent-testing/guides/runner.md
```

Expected: new paths present, old paths absent, Apifox guidance still present.

## Task 5: Update Specialized Guides and Report References

**Files:**
- Modify: `docs/agent-testing/guides/concurrency.md`
- Modify: `docs/agent-testing/guides/performance.md`
- Modify: `docs/agent-testing/guides/subagent.md`
- Modify: `docs/agent-testing/guides/environment.md`
- Modify: `docs/agent-testing/guides/go-runner.md`
- Modify: `docs/agent-testing/reports/README.md`
- Modify: `docs/agent-testing/templates/module.md`

- [ ] **Step 1: Add protocol references**

In each assigned file, add a short reference near the top when not already present:

```markdown
通用计划字段、依赖授权、数据隔离、证据、报告、清理、敏感信息和失败输出规则见 `docs/agent-testing/templates/protocol.md`。本文只记录本主题的附加规则。
```

For `reports/README.md`, use:

```markdown
通用证据、清理和敏感信息规则见 `docs/agent-testing/templates/protocol.md`。本文保留报告命名、字段和模板。
```

For `templates/module.md`, use:

```markdown
通用依赖授权、证据、清理和敏感信息规则见 `docs/agent-testing/templates/protocol.md`。本模板只描述单模块测试契约结构。
```

- [ ] **Step 2: Update old path references**

Replace old paths:

```text
docs/agent-testing/guides/concurrency-consistency.md -> docs/agent-testing/guides/concurrency.md
guides/concurrency-consistency.md -> guides/concurrency.md
docs/agent-testing/guides/performance-load.md -> docs/agent-testing/guides/performance.md
guides/performance-load.md -> guides/performance.md
```

- [ ] **Step 3: Do not aggressively delete content**

This task should not attempt a full rewrite. Keep specialized guides readable. Only remove a paragraph if it is a pure duplicate and the surrounding text already points to `templates/protocol.md`.

- [ ] **Step 4: Verify references**

Run:

```bash
rtk rg -n "templates/protocol.md" docs/agent-testing/guides docs/agent-testing/reports/README.md docs/agent-testing/templates/module.md
rtk rg -n "concurrency-consistency|performance-load" docs/agent-testing
```

Expected: protocol references present; old long names absent.

## Task 6: Cross-Document Verification

**Files:**
- Read all changed `docs/agent-testing` files.

- [ ] **Step 1: Verify old names are gone**

Run:

```bash
rtk rg -n "concurrency-consistency|performance-load" docs/agent-testing
```

Expected: no matches.

- [ ] **Step 2: Verify new names and protocol are routed**

Run:

```bash
rtk rg -n "templates/protocol.md|guides/concurrency.md|guides/performance.md" docs/agent-testing/README.md docs/agent-testing/guides docs/agent-testing/reports/README.md docs/agent-testing/templates/module.md
```

Expected: README, runner, specialized guides, report README, and module template reference new paths.

- [ ] **Step 3: Verify key safety rules remain**

Run:

```bash
rtk rg -n "并发一致性测试必须|执行许可|不得连接真实|真实依赖|测试数据隔离|主 agent 复核|敏感信息|DROP|TRUNCATE|FLUSHALL|FLUSHDB" docs/agent-testing
```

Expected: safety rules are still present, especially in `templates/protocol.md`, `guides/concurrency.md`, and `guides/subagent.md`.

- [ ] **Step 4: Verify no incomplete markers**

Run:

```bash
rtk rg -n "TB[D]|TO[D]O|待[定]|FIXM[E]|placeholde[r]" docs/agent-testing/README.md docs/agent-testing/guides docs/agent-testing/templates docs/agent-testing/reports/README.md
```

Expected: no matches.

- [ ] **Step 5: Review final file list**

Run:

```bash
rtk find docs/agent-testing -maxdepth 3 -type f | sort
rtk git status --short docs/agent-testing
```

Expected: `templates/protocol.md`, `guides/concurrency.md`, and `guides/performance.md` exist. Old long-name files do not exist.

## Task 7: Commit

**Files:**
- Stage only files changed for this implementation.

- [ ] **Step 1: Review staged candidate files**

Run:

```bash
rtk git status --short docs/agent-testing docs/superpowers/specs/2026-05-31-agent-testing-keep-simple-design.md docs/superpowers/plans/2026-05-31-agent-testing-keep-simple.md
```

Expected: dirty files may include pre-existing work. Stage only keep-simple implementation files.

- [ ] **Step 2: Stage implementation**

Run:

```bash
rtk git add docs/agent-testing/README.md docs/agent-testing/guides/runner.md docs/agent-testing/guides/concurrency.md docs/agent-testing/guides/performance.md docs/agent-testing/guides/subagent.md docs/agent-testing/guides/environment.md docs/agent-testing/guides/go-runner.md docs/agent-testing/reports/README.md docs/agent-testing/templates/module.md docs/agent-testing/templates/protocol.md docs/superpowers/specs/2026-05-31-agent-testing-keep-simple-design.md docs/superpowers/plans/2026-05-31-agent-testing-keep-simple.md
```

If old long-name files still appear as tracked or staged, handle them according to git status:

```bash
rtk git add docs/agent-testing/guides/concurrency-consistency.md docs/agent-testing/guides/performance-load.md
```

- [ ] **Step 3: Confirm staged scope**

Run:

```bash
rtk git diff --cached --name-status
```

Expected staged files are only agent-testing docs and the keep-simple spec/plan. No module code, flow contracts, existing reports, or runner.go should be staged.

- [ ] **Step 4: Commit**

Run:

```bash
rtk git commit -m "docs(agent-testing): simplify protocol routing"
```

Expected: commit succeeds.
