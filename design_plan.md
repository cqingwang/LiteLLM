# 请求详细日志与镜像构建改造方案

> 本文档是实施计划，不在本阶段直接修改业务代码。实施时按任务顺序执行，每个任务完成后运行对应的验证命令，再进入下一项。

**目标：** 在“使用日志”的普通请求日志中增加“详细信息”入口，持久化请求/响应原始内容并在独立详情页中以类似 Postman 的方式预览；同时在项目根目录增加 `deploy.sh --build` 构建镜像，以及 `deploy.sh --dev` 复用镜像启动本地编译产物。详情入口必须在日志后台刷新期间仍可点击，正文展示必须保留原始格式。

**架构：** 保留现有 `logs` 作为计费、筛选和列表主表，新增与请求日志一对一关联的请求详情表。主表仅保存稳定的详情关联 ID，列表接口返回该 ID；详情接口按当前登录用户/管理员权限校验后读取详情表。请求详情采集放在统一的 relay 请求生命周期边界，避免在各 provider adapter 中重复实现，并通过大小限制、敏感字段脱敏和失败降级保护转发链路。前端新增独立详情路由，列表中的“详细信息”列只负责跳转，不再把大 payload 塞进列表响应。

**技术栈：** Go、Gin、GORM v2、SQLite/MySQL/PostgreSQL；现有日志库配置下兼容 ClickHouse；React 19、TypeScript、TanStack Router、React Query、Tailwind、现有 JSON/代码高亮能力；Dockerfile 作为镜像唯一构建上下文。

---

## 一、现状与已确认边界

### 1. 现有请求日志链路

- 后端列表接口：
  - 管理员：`GET /api/log/`，入口为 `controller.GetAllLogs`。
  - 普通用户：`GET /api/log/self`，入口为 `controller.GetUserLogs`。
- 主日志实体与写入：`model/log.go` 的 `Log`、`RecordConsumeLog`、`RecordTaskBillingLog`；普通使用日志由 `RecordConsumeLog` 写入。
- 前端功能目录：`web/src/features/usage-logs/`。
  - 请求封装：`api.ts`。
  - 列表与分页：`components/usage-logs-table.tsx`、`lib/utils.ts`。
  - 普通日志列：`components/columns/common-logs-columns.tsx`。
  - 现有计费/审计详情弹窗：`components/dialogs/details-dialog.tsx`。
  - 当前路由：`web/src/routes/_authenticated/usage-logs/$section.tsx`。
- 数据库初始化/迁移集中在 `model/main.go`，主数据库和可选日志数据库需要分别考虑。

### 2. 必须保留的兼容性

- 不改变已有 `/api/log` 列表接口的分页、筛选和脱敏语义；新增字段应允许历史日志为空。
- SQLite、MySQL、PostgreSQL 均必须可迁移、可查询；不能使用某一数据库独有的 JSON/BLOB/外键特性作为唯一实现。
- 项目当前可选 ClickHouse 日志库的 `logs.id` 可能不是稳定自增主键，现有代码在 ClickHouse 场景会给列表行分配分页展示序号。因此详情关联不能只依赖这个展示 ID，必须增加独立、稳定、不可猜测的 `detail_id`（建议 UUID/随机字符串），并在接口中把它作为详情查询键。
- 原始请求内容可能包含 API key、Cookie、Authorization、图片/音频/文件和超大上下文；不能默认完整无界保存，也不能把敏感 header 原样写入数据库或返回前端。
- 不修改项目已有受保护的品牌、组织、版权和归属信息；镜像标签按用户要求使用 `cqingwang/litellm:latest`。

### 3. 推荐的默认产品行为

- “详细信息”列仅出现在 `common` 普通请求日志中；绘图/异步任务日志暂不接入，除非后续确认它们也需要完整请求/响应审计。
- 只对实际形成普通请求消费日志的请求建立详情记录；请求在鉴权、参数校验或选路前失败时，保留现有错误日志行为，不为了详情采集改变失败链路。
- 详情内容分为 `request`、`response`、`error` 三类，并保存 HTTP 方法、路径、状态码、Content-Type、是否流式、耗时、请求 ID、上游请求 ID、截断标志和采集时间等元数据。
- 超过配置上限时只保存前缀并标记 `truncated=true`；二进制内容只保存安全摘要/文本化预览，不把任意二进制直接当 UTF-8 展示。
- 详情表写入失败只告警并让原请求继续完成；主日志写入时没有有效详情 ID 时，列表中的入口显示不可用状态，不阻断计费。

## 二、数据模型与安全设计

### 1. 新增请求详情表

建议新增 `model/request_log_detail.go`，实体名使用 `RequestLogDetail`，表名建议 `request_log_details`。字段按跨数据库可用类型设计：

- `id`：稳定主键，建议字符串 UUID/随机 ID；不使用 ClickHouse 的展示序号。
- `log_id`：关联主日志的数据库 ID，仅在 SQLite/MySQL/PostgreSQL 中作为辅助关联；ClickHouse 场景允许为空。
- `request_id`、`upstream_request_id`：复制关联请求标识并建立索引，支持排障和回查。
- `user_id`：详情归属用户，建立索引，用于权限过滤。
- `request_method`、`request_path`、`request_content_type`、`response_content_type`。
- `request_headers`、`response_headers`：脱敏后的 JSON 字符串；只保存允许展示的 header 白名单或已脱敏键值。
- `request_body`、`response_body`、`error_body`：TEXT/LONGTEXT 兼容字段，保存安全文本；不要依赖数据库 JSON 类型。
- `request_size`、`response_size`、`status_code`、`duration_ms`、`is_stream`、`request_truncated`、`response_truncated`。
- `created_at`、`updated_at`：沿用项目时间戳约定。

建议在 `model/main.go` 中增加统一迁移入口，并为 `user_id`、`request_id`、`created_at` 建索引。是否建立物理外键应以当前项目迁移策略为准；默认不建立跨日志库外键，依赖应用层一致性，避免 ClickHouse/SQLite 兼容问题。

### 2. 主日志关联字段

在 `model.Log` 增加可空/默认空的 `DetailId string`，数据库列名使用 `detail_id` 并建立索引。`RecordConsumeLog` 的流程调整为：

1. 统一采集器先生成 `detail_id` 并记录详情，或在详情需要延迟补齐时先生成 ID。
2. 生成 `Log` 时将 `DetailId` 写入主日志。
3. 任一详情写入失败时，仍写入主日志，`DetailId` 保持空并记录告警。

更稳妥的实现是把“详情记录”和“消费主日志”放进同一个主数据库事务（在主日志使用 ClickHouse 时则不能假设跨库事务），因此必须根据 `common.UsingLogDatabase(...)` 分支明确一致性策略：主数据库模式尽量事务化，ClickHouse 模式使用先写详情/后写主日志的幂等补偿策略，并以 `request_id + detail_id` 防止重复记录。

### 3. 采集与脱敏策略

新增 `service/request_log_detail.go` 或同等职责的独立服务，提供单一写入入口，不允许各 adapter 自行拼装详情表记录。具体策略：

- 从现有 `common.KeyRequestBody`、请求 ID 上下文和 relay 响应生命周期获取原始数据；先确认当前 streaming/non-streaming 路径实际可获得的字节范围，再决定采集器挂载点。
- header 采用白名单或统一脱敏：至少隐藏 `Authorization`、`Cookie`、`Set-Cookie`、API key 相关 header；URL query 中的 token/key 也必须清理。
- JSON 文本尽量格式化预览，但数据库保留安全后的原始文本；格式化失败时按纯文本显示。
- SSE/流式响应保留原始事件文本并标记 `is_stream=true`，前端同时支持“原始 SSE”和“解析后的 JSON/文本”预览。
- multipart、图片、音频和文件请求只保存 Content-Type、大小、文件名（如安全）及可选摘要；不把上传凭据或二进制内容直接落库。
- 上限配置外置，缺失关键安全配置时采用项目明确的安全失败策略；至少提供请求体、响应体、单条详情和保留天数上限，并在达到上限时记录截断标志。
- 日志详情接口按 `detail_id` 查询，并在 SQL 条件中同时附加管理员权限或 `user_id = 当前用户`；禁止仅凭前端传入的 ID 返回详情。

## 三、后端实施步骤

### 任务 1：补充请求详情领域模型与迁移

**文件：**

- 新建：`model/request_log_detail.go`。
- 修改：`model/log.go`，为 `Log` 增加 `DetailId`，为消费日志写入链路预留关联。
- 修改：`model/main.go`，接入 `request_log_details` 的 AutoMigrate/兼容迁移与索引。
- 参考：`model/main.go` 现有 SQLite、MySQL、PostgreSQL、ClickHouse 初始化分支。

**实施要点：** 使用 GORM 模型和项目现有数据库初始化方式；避免 raw SQL，确需 SQL 时为三种主数据库提供分支。明确历史 `logs` 记录的 `detail_id=''` 合法，迁移重复执行不产生错误。

**验证：** 使用 SQLite 临时数据库执行初始化和重复初始化；在 MySQL/PostgreSQL 测试环境分别执行迁移；检查 `go test ./model/...` 或项目实际模型测试集、`git diff --check`。

### 任务 2：实现统一详情采集、脱敏和大小限制

**文件：**

- 新建：`service/request_log_detail.go` 及其测试文件（建议放在现有 service 测试布局中）。
- 可能修改：`common/` 中配置读取位置、`controller/relay.go`、`relay/relay_task.go` 或实际统一响应出口；以源码追踪后的真实生命周期为准，不在 provider adapter 中复制逻辑。

**实施要点：** 先覆盖普通 Chat/Responses、非流式和流式请求，再评估音频、图片、任务、WebSocket 是否能安全复用。采集服务必须具备幂等写入、超限截断、二进制降级、header 脱敏和异常不阻断主请求能力。

**验证：** 为脱敏、截断、JSON/纯文本、SSE、二进制和写入失败分别写确定性单测；确认输入中 Authorization/Cookie/query key 不会出现在持久化值和 API 响应中。先用最小 fixture 证明缺陷/边界，再运行受影响的 Go 测试。

### 任务 3：把详情 ID 接入请求日志写入链路

**文件：**

- 修改：`model/log.go` 的 `RecordConsumeLogParams`、`RecordConsumeLog`。
- 修改：统一 relay/service 调用点，使请求详情和主日志使用同一 `detail_id`。
- 可能修改：错误/任务日志路径，只有确认它们会进入普通请求日志且能拿到可靠响应内容时才纳入。

**实施要点：** 保持现有计费、预扣费、结算和 DataExport 行为不变。详情写入不得改变主日志是否写入，也不得因为 payload 太大增加同步阻塞；必要时使用有界缓冲或在请求结束前完成最小元数据写入、正文异步补齐，但必须保证详情页不会显示“已关联但永远不存在”的 ID。

**验证：** 增加主日志-详情关联单测/集成测试，覆盖详情成功、详情失败、历史日志空 ID、重复 request ID、流式响应结束和上游错误响应；确认主日志的 quota/token/other 等现有字段未改变。

### 任务 4：新增详情查询 API 与权限边界

**文件：**

- 修改：`controller/log.go`，增加管理员和用户详情查询 handler，或实现一个按权限复用的 handler。
- 修改：`router/api-router.go`，增加例如 `GET /api/log/detail/:detail_id` 的受保护路由。
- 修改/新建：`model/request_log_detail.go` 查询方法和 DTO，返回前端所需的安全结构，而非直接序列化 GORM 实体。

**实施要点：**

- 管理员可查看全量；普通用户只能查看自己的详情。
- 不允许通过 `log_id`、分页展示序号或可猜测自增 ID 越权读取。
- 不存在、无权限、已过期分别映射到稳定的 404/403/业务错误语义，避免泄漏其他用户记录是否存在。
- 详情响应保持字段类型稳定；body 过大时服务端仍遵守存储截断标志。

**验证：** 使用 Gin/httptest 覆盖管理员、详情归属用户、其他用户、空 ID、非法 ID、历史主日志和数据库错误；确认未认证请求被现有 middleware 拦截。

## 四、前端实施步骤

### 任务 5：增加列表“详细信息”列与类型/API

**文件：**

- 修改：`web/src/features/usage-logs/data/schema.ts`，增加 `detail_id` 可空字段。
- 修改：`web/src/features/usage-logs/types.ts`，增加详情响应类型。
- 修改：`web/src/features/usage-logs/api.ts`，增加 `getLogDetail(detailId)`，继续使用统一 `api` 实例。
- 修改：`web/src/features/usage-logs/components/columns/common-logs-columns.tsx`，新增“详细信息”列。
- 可能修改：`web/src/features/usage-logs/lib/columns.ts`、移动端卡片组件，确保桌面列与移动端均可进入详情页。
- 修改：`web/src/i18n/locales/en.json`、`zh.json`、`zh-TW.json`、`fr.json`、`ru.json`、`ja.json`、`vi.json`，补齐列名、按钮、状态、错误和预览文案。

**实施要点：** 只有 `detail_id` 非空时显示可访问的 Link/按钮；历史日志显示“无详细信息”。不要把详情正文放入列表 API 或表格行状态。列应使用 `useNavigate`/类型安全 `Link`，并支持键盘访问、`aria-label` 和移动端等价入口。

### 任务 6：新增独立详情路由和 Postman 风格预览页

**文件：**

- 新建：`web/src/routes/_authenticated/usage-logs/detail/$detailId.tsx`（最终路径按 TanStack Router 生成规则校准）。
- 新建：`web/src/features/usage-logs/components/request-log-detail-page.tsx`。
- 新建：`web/src/features/usage-logs/components/request-log-payload-viewer.tsx`，必要时拆分 header/metadata/body 子组件。
- 复用：现有 `web/src/features/usage-logs/components/dialogs/details-dialog.tsx` 中的复制、格式化和状态展示能力，但不要把现有业务计费弹窗强行改造成整页。

**页面结构：**

- 顶部：返回使用日志、请求 ID/上游请求 ID、HTTP 状态、耗时、流式标志、时间。
- 左右或上下分栏：Request / Response；分别展示 method、URL/path、headers、query（已脱敏）、body。
- body 预览：自动识别 JSON、纯文本、SSE；提供格式化/原始切换、复制按钮、截断提示和空内容状态。
- 错误响应独立突出显示；不使用 `dangerouslySetInnerHTML` 渲染未信任内容，代码高亮仅使用现有安全渲染链路。
- 加载中、404/403、接口失败、空详情、超长内容和窄屏布局都要有明确状态。

**验证：** 使用 React Testing Library/Vitest 从用户视角验证点击列入口、路由跳转、请求成功展示、JSON/文本/SSE 降级、复制、返回、权限错误、截断提示和移动端可见入口；执行受影响测试、`bun run typecheck`、相关文件 lint 和 `bun run build:check`。

## 五、根目录 `deploy.sh --build`

### 任务 7：实现构建命令

**文件：**

- 新建：`/Users/chan/Documents/project/ML/LiteLLM/deploy.sh`。
- 参考：`Dockerfile`、`Dockerfile.dev`、`docker-compose.yml`、`README.md` 的现有部署说明。

**命令契约：**

```bash
./deploy.sh --build
```

`--build` 从脚本所在项目根目录作为 Docker build context，调用仓库现有 `Dockerfile`，构建并标记：

```text
cqingwang/litellm:latest
```

建议脚本具备以下行为：

- `set -Eeuo pipefail`，命令失败立即退出并保留 Docker 原始错误。
- 通过 `SCRIPT_DIR` 固定 context，脚本从任意当前目录执行都不会把错误目录作为 context。
- 启动前检查 `docker` 命令和 Docker daemon 可用性；缺失时给出明确错误。
- `--build` 和 `--dev` 是已承诺的子命令；未知参数、缺失参数显示用法并返回非零。
- 默认只 build/tag，不隐式 push、不停止或删除现有容器、不覆盖数据卷；如未来增加 push 必须另设显式命令。
- 是否使用 `--pull`、BuildKit 和平台参数要与当前发布环境确认；默认不擅自改变 Dockerfile 的 `TARGETOS/TARGETARCH` 行为。若需要跨平台构建，使用显式 `--platform`/build args 并在文档中记录。
- 构建成功后打印完整镜像标签及 `docker image inspect` 的 ID/创建时间，便于确认确实生成了目标镜像。

`--dev` 不执行 Docker build，而是按源码时间戳增量准备本地 Linux/amd64 运行文件：前端源码变化时执行本地 `bun run build`，Go 源码或前端产物变化时执行本地交叉编译，随后把 `.dev/new-api` 只读挂载到 `cqingwang/litellm:latest` 的 `/new-api`，并把本地 `data` 挂载到 `/data`。由于前端通过 Go `embed` 编译进二进制，不单独挂载 `web/dist`；每次执行都会删除同名调试容器后重新启动，便于调试，数据通过本地 `data` 挂载保留。可通过 `LITELLM_DEV_CONTAINER` 和 `LITELLM_DEV_PORT` 调整容器名与宿主端口。

**验证：**

```bash
bash -n deploy.sh
./deploy.sh --unknown                 # 应失败并打印用法
./deploy.sh --build                    # 真实构建
docker image inspect cqingwang/litellm:latest
```

真实构建需在有 Docker daemon、网络和足够磁盘空间的环境执行；仅 `bash -n` 不能替代镜像构建成功证据。构建完成后确认镜像中的 `/new-api`、前端产物、`/data` 工作目录与现有 Dockerfile 预期一致。

## 六、测试与验收标准

### 后端

- `go test ./...` 通过，至少包含新增模型、迁移、脱敏/截断、关联写入、详情查询权限测试。
- 使用 SQLite 完成一轮迁移、写入、列表返回 `detail_id`、详情查询和重复迁移。
- 在 MySQL/PostgreSQL 环境各完成迁移和详情查询验证；如项目 CI 有对应数据库 job，加入同等覆盖。
- 若启用 ClickHouse 日志库，验证不依赖 `logs.id` 的展示序号，详情仍通过稳定 `detail_id` 查询。
- `git diff --check` 通过；检查日志和 API 响应中不存在 Authorization、Cookie、API key 等敏感值。

### 前端

- `cd web && bun run test -- ...`（至少运行新增/受影响测试文件）。
- `cd web && bun run typecheck`。
- `cd web && bun run lint`，或按项目约定对受影响文件执行 lint。
- `cd web && bun run build:check`。
- 手工验收：历史日志、成功非流式、成功流式、上游错误、超限截断、无权限详情、移动端详情页。

### 镜像

- `bash -n deploy.sh` 通过。
- `./deploy.sh --build` 真实完成且 `docker image inspect cqingwang/litellm:latest` 成功。
- 用该镜像启动临时容器，检查 `/api/status`、前端入口和已存在的数据库/Redis 配置契约；不在构建脚本中写入凭据。

## 七、风险、取舍与开放问题

1. **采集挂载点风险（高）：** relay 有普通、Responses、Claude、Gemini、音频、任务、WebSocket 和流式分支。实施任务 2 前必须用调用链确认统一出口；若无法覆盖所有格式，应先明确支持矩阵，不要在各 provider 中复制一套不一致的采集逻辑。
2. **敏感数据与合规风险（高）：** 请求/响应可能包含个人数据、密钥、文件和上下文。默认应偏向脱敏、限长和可配置保留期；是否允许管理员查看完整 body、是否需要加密存储、是否需要删除接口，需要产品/部署方明确。
3. **存储膨胀风险（高）：** 详情正文可能远大于 `logs`。需要在实施前确定最大请求/响应字节数、保留天数、清理任务是否复用现有日志清理，及是否允许关闭详情采集。
4. **一致性风险（中）：** 主日志可能位于 ClickHouse，详情表若位于主数据库则无法依赖跨库事务。方案采用稳定 ID、幂等写入和“详情缺失不阻断计费”的可观测降级；若业务要求强一致，应先限定数据库部署模式。
5. **前端依赖风险（低）：** 当前已有 `marked`、CodeMirror/Shiki 等能力，但详情预览不应为“类似 Postman”新增重量级依赖；优先复用已安装依赖，只有现有能力不足时才评估新增包。
6. **镜像命名风险（中）：** 仓库源码、模块路径和 UI 仍保持现有项目身份；`cqingwang/litellm:latest` 仅作为用户指定的 Docker tag，不应借此批量替换 README、模块路径或版权归属。

## 八、推荐实施顺序

1. 先完成任务 1，确定三种数据库与 ClickHouse 的字段/迁移行为。
2. 完成任务 2，锁定安全、大小和流式采集契约，并先让单测覆盖边界。
3. 完成任务 3、4，打通后端真实数据链路和权限。
4. 完成任务 5、6，接入列表、详情路由和预览交互。
5. 完成任务 7，增加构建脚本并进行真实镜像验证。
6. 按第六节执行完整验收；若详情采集仍不能覆盖某种 relay 格式，明确标注为部分支持，不宣称所有请求格式均已完成。
