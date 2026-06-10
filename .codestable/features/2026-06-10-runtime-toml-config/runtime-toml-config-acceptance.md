---
doc_type: feature-acceptance
feature: 2026-06-10-runtime-toml-config
status: accepted
summary: 验收启动 TOML 配置、默认 ~/.webot-msg 存储路径与日志文件大小策略
tags: [config, cli, logging]
---

# runtime-toml-config 验收报告

> 阶段：阶段 3（验收闭环）
> 验收日期：2026-06-10
> 关联方案 doc：`.codestable/features/2026-06-10-runtime-toml-config/runtime-toml-config-design.md`

## 1. 接口契约核对

对照方案第 2.1 节名词层逐一核查：

**接口示例逐项核对**：
- [x] 未传 `-c`：`loadRuntimeConfig("")` 返回 `runtimeconfig.Default()`，默认 auth store 为 `~/.webot-msg/config/auth.json`，默认日志为 `~/.webot-msg/logs/webot-msg.log`。代码：`cmd/webot-msg/main.go:58`、`internal/runtimeconfig/config.go:51`；测试：`internal/runtimeconfig/config_test.go:13`。
- [x] 传入 `-c ~/.webot-msg/config/webot-msg.toml`：`LoadFile` 先展开配置文件路径，再用 TOML 覆盖默认值，最终 `app.New(authPath, baseURL)` 与 `app.Run(port)` 使用 resolved config。代码：`cmd/webot-msg/main.go:17`、`cmd/webot-msg/main.go:41`、`internal/runtimeconfig/config.go:69`。
- [x] `storage.auth_path = "~/.webot-msg/config/auth.json"`：`Resolve` 通过 `expandHome` 展开到当前用户 home；`PrepareStorage` 创建父目录。代码：`internal/runtimeconfig/config.go:103`、`internal/runtimeconfig/config.go:135`；测试：`internal/runtimeconfig/config_test.go:76`。
- [x] `log.max_size = "1GB"`：`ParseSize` 按二进制单位解析为 `1073741824`。代码：`internal/runtimeconfig/config.go:261`；测试：`internal/runtimeconfig/config_test.go:312`。
- [x] `log.max_size = "10XB"`：`Resolve` 返回 `log.max_size` 字段错误。代码：`internal/runtimeconfig/config.go:118`；测试：`internal/runtimeconfig/config_test.go:258`。

**名词层“现状 → 变化”逐项核对**：
- [x] Runtime config 独立于 Auth store：新增 `internal/runtimeconfig.Config`，`config.UserConfig` 未新增启动配置字段。代码：`internal/runtimeconfig/config.go:26`、`internal/config/store.go:21`。
- [x] TOML 配置契约包含 `api.port`、`storage.auth_path`、`ilink.base_url`、`log.file_path`、`log.max_size`。代码：`internal/runtimeconfig/config.go:33`、`internal/runtimeconfig/config.go:37`、`internal/runtimeconfig/config.go:41`、`internal/runtimeconfig/config.go:45`。
- [x] 默认存储根目录统一为 `~/.webot-msg/`，按 `config/` 与 `logs/` 划分。代码：`internal/runtimeconfig/config.go:18`、`internal/runtimeconfig/config.go:19`。
- [x] legacy auth copy 只在默认 auth path、旧文件存在、新文件不存在时执行，不覆盖新文件。代码：`internal/runtimeconfig/config.go:151`；测试：`internal/runtimeconfig/config_test.go:104`、`internal/runtimeconfig/config_test.go:145`。
- [x] auth store 权限 owner-only：默认 auth 目录、legacy copy 目标文件、`Store.Save` 创建和回写文件均覆盖。代码：`internal/runtimeconfig/config.go:135`、`internal/runtimeconfig/config.go:208`、`internal/config/store.go:17`、`internal/config/store.go:207`；测试：`internal/runtimeconfig/config_test.go:104`、`internal/config/store_test.go:9`。

**流程图核对**：
- [x] `解析 flags` → `加载 Runtime config` → `应用 -port 覆盖` → `PrepareStorage` → `NewSizeWriter` → `app.New` / `Run` 在 `cmd/webot-msg/main.go` 中均有落点。
- [x] `legacy auth copy`、日志启停分支、配置校验和目录准备在 `internal/runtimeconfig` 与 `internal/logfile` 中有独立落点，没有塞进 auth store 或 app 编排层。

## 2. 行为与决策核对

**需求摘要逐项验证**：
- [x] `-c` 读取 TOML 并覆盖默认值：`LoadFile` 用默认配置作为基底，再解码 TOML；测试覆盖只写 `api.port` 其余字段回落默认值。
- [x] API port、auth store path、iLink BaseURL、日志路径、日志大小上限均可由 Runtime config 表达。
- [x] 所有默认存储路径落在 `~/.webot-msg/` 下：默认 auth path 与 log path 已常量化。
- [x] `-port` 保留为兼容覆盖项：`flagIsSet("port")` 为 true 时覆盖 TOML `api.port`；测试覆盖 `18080` 被 `19090` 覆盖。
- [x] 日志文件大小策略已实现：`SizeWriter` 在下一次写入会超过上限时轮转为 `.1`，避免单个日志文件无限追加。

**明确不做逐项核对**：
- [x] 未改变 auth store JSON schema；`UserConfig` 未出现 Runtime config 字段。
- [x] 未新增环境变量配置入口；`rg "os\\.Getenv|LookupEnv" cmd internal` 无命中。
- [x] 未改变 HTTP API endpoint、请求参数或 token 校验语义；`internal/api` 未在本次 diff 中变更。
- [x] 未把 token 或完整消息正文主动写入日志文件；新增 `log.Printf` 只记录端口、路径、BaseURL、大小和 legacy copy 源/目标。
- [x] 未实现日志保留天数、压缩归档、远程日志采集等日志平台能力；`SizeWriter` 仅保留单个 `.1` 备份。

**关键决策落地**：
- [x] Runtime config 与 Auth store 分离：`internal/runtimeconfig` 只处理启动配置，`internal/config.Store` 继续处理 bot 状态。
- [x] 严格 TOML：`MetaData.Undecoded()` 被检查，未知 key 启动失败。
- [x] `ilink.base_url` 限定为 `http` / `https` 且必须包含 host。
- [x] 默认 auth store 迁移兼容：旧 `./config/auth.json` 作为一次性复制源，不作为同步源。
- [x] 凭据权限收紧：auth 目录和文件 owner-only，`Store.Save` 会把已有宽权限 auth 文件收紧到 `0600`。

**编排层“现状 → 变化”逐项核对**：
- [x] CLI 入口不再直接使用 `config.DefaultPath` / `ilink.DefaultBaseURL` 创建 app，而是通过 resolved Runtime config 注入最终值。
- [x] `app.Run` 仍只接收最终端口，app 主业务编排不解析 TOML，不知道配置文件存在。
- [x] 文件日志在 app 创建前初始化；日志 writer 出错会在登录、监听或 HTTP API 启动前失败。

**流程级约束核对**：
- [x] 字段级错误：`api.port`、`storage.auth_path`、`ilink.base_url`、`log.file_path`、`log.max_size` 都有可定位错误前缀。
- [x] 兼容性：未传 `-c` 不要求默认 TOML 存在；显式 `-port` 优先；legacy copy 不覆盖新 auth。
- [x] 安全性：Runtime config 不保存凭据；auth 文件权限 owner-only；新增文件日志不接管控制台消息内容。
- [x] 可观测性：启用文件日志后记录启动摘要和 legacy copy 事件，不记录 token 或消息正文。

**挂载点反向核对（可卸载性）**：
- [x] 挂载点 M1：CLI flags。实际落点：`cmd/webot-msg/main.go:13`、`cmd/webot-msg/main.go:14`。
- [x] 挂载点 M2：TOML key。实际落点：`internal/runtimeconfig/config.go:33` 到 `internal/runtimeconfig/config.go:48`。
- [x] 挂载点 M3：默认存储根目录。实际落点：`internal/runtimeconfig/config.go:18`、`internal/runtimeconfig/config.go:19`。
- [x] 挂载点 M4：日志初始化点。实际落点：`cmd/webot-msg/main.go:27`。
- [x] 反向 grep：`rg "runtimeconfig|logfile|DefaultAuthPath|DefaultLogPath|LoadFile|PrepareStorage|NewSizeWriter" cmd internal` 命中均落在 `cmd/webot-msg`、`internal/runtimeconfig`、`internal/logfile` 和相关测试内，未发现清单外业务挂载点。
- [x] 拔除沙盘推演：移除本 feature 时需要还原 `cmd/webot-msg/main.go` 的 flag/config/log 编排，删除 `internal/runtimeconfig`、`internal/logfile`、新增测试与 TOML 依赖，并把 `internal/config/store.go` 的权限策略按是否保留安全修复单独决策；没有隐藏在 HTTP API 或 app 业务层的额外入口。

## 3. 验收场景核对

- [x] 不传 `-c`：默认使用 `~/.webot-msg/config/auth.json`、`~/.webot-msg/logs/webot-msg.log`、默认 iLink BaseURL。证据：`TestLoadFileMergesDefaults`、`TestResolveExpandsHomeAndParsesLogSize`。
- [x] legacy auth 存在且新默认 auth 不存在：启动准备阶段原样复制旧文件。证据：`TestPrepareStorageCopiesLegacyAuth`。
- [x] legacy auth 与新默认 auth 都存在：不覆盖新文件。证据：`TestPrepareStorageDoesNotOverwriteExistingAuth`。
- [x] TOML `api.port = 18080`：最终配置端口为 `18080`。证据：`TestBuildRuntimeConfigUsesConfigPort`。
- [x] TOML 缺省 `storage.auth_path` 与 `log.file_path`：回落到 `~/.webot-msg/config/auth.json` 与 `~/.webot-msg/logs/webot-msg.log`。证据：`TestLoadFileMergesDefaults`。
- [x] `storage.auth_path = "~/.webot-msg/config/auth.json"`：路径展开到当前用户 home，父目录不存在时创建。证据：`TestResolveExpandsHomeAndParsesLogSize`、`TestPrepareStorageCopiesLegacyAuth`。
- [x] `storage.auth_path = "./tmp/auth.json"` / 显式旧路径：auth store 使用显式路径，不触发 legacy 默认迁移。证据：`TestPrepareStorageKeepsExplicitLegacyAuthPath`。
- [x] `ilink.base_url = "https://example.com"`：通过校验并传给 `app.New`；endpoint 仍由 iLink client 使用 `BaseURL` 组装。证据：`internal/runtimeconfig/config.go:97`、`cmd/webot-msg/main.go:41`。
- [x] `ilink.base_url = "ftp://example.com"` 或 `"file://host"`：启动期拒绝并定位 `ilink.base_url`。证据：`TestResolveRejectsInvalidValues`。
- [x] 未知 TOML section/key：启动期拒绝并指出未知 key。证据：`TestLoadFileRejectsUnknownKeys`。
- [x] `log.file_path` 非空且 `log.max_size = "10MB"`：标准日志写入文件，超过上限触发大小控制。证据：`TestSizeWriterRotatesWhenMaxSizeWouldBeExceeded`。
- [x] `log.max_size = "1GB"`：解析为 `1073741824`。证据：`TestResolveExpandsHomeAndParsesLogSize`、`TestParseSize`。
- [x] `log.max_size = "abc"`、`api.port = -1` 或 `~` 无法展开：启动期失败并带字段前缀。证据：`TestResolveRejectsInvalidValues`。
- [x] 同时传 `-c` 且显式 `-port`：最终端口使用 `-port`。证据：`TestBuildRuntimeConfigPortFlagOverridesConfig`。
- [x] 前端验证：本 feature 无前端改动，不需要浏览器肉眼验证。

验证命令：
- `go test ./...`：通过。
- `go vet ./...`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-06-10-runtime-toml-config/runtime-toml-config-checklist.yaml --yaml-only`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/features/2026-06-10-runtime-toml-config/runtime-toml-config-design.md`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/architecture/ARCHITECTURE.md`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/requirements/bot-message-bridge.md`：通过。
- `python3 .codestable/tools/validate-yaml.py --file .codestable/requirements/VISION.md`：通过。

## 4. 术语一致性

- Runtime config：代码中使用 package `internal/runtimeconfig` 与 `runtimeconfig.Config`，与方案术语一致。
- Storage base / `~/.webot-msg/`：默认路径常量为 `~/.webot-msg/config/auth.json` 和 `~/.webot-msg/logs/webot-msg.log`，无默认 `./logs` 或默认工作目录存储路径。
- Auth store：仍由 `internal/config.Store` 管理，字段名仍是 `BotToken`、`APIToken`、`ContextToken` 等 bot 状态字段，没有混入启动配置。
- Log file policy：代码中落实为 `internal/logfile.SizeWriter`，配置名为 `log.file_path` / `log.max_size`。
- `-c`：CLI flag 名称、设计文档和 checklist 一致。
- 防冲突：`rg "RuntimeConfig" cmd internal` 无历史类型冲突；`config.AppConfig` 未被复用为启动配置。

## 5. 架构归并

对照方案第 4 节，已实际更新 `.codestable/architecture/ARCHITECTURE.md`：

- [x] 入口描述：从“解析 `-port` 后用默认配置路径和 iLink base URL 创建应用”更新为“解析 `-c` / `-port`，加载 Runtime config，准备存储和日志后创建应用”。
- [x] 名词归并：新增 Runtime config 与 Log file policy 术语。
- [x] 数据与状态：说明 auth store JSON schema 不变，默认 auth store 从 `./config/auth.json` 迁到 `~/.webot-msg/config/auth.json`；Runtime config 是独立 TOML，不保存凭据。
- [x] 动词骨架归并：补充 `internal/runtimeconfig` 和 `internal/logfile` 在启动路径中的职责。
- [x] 流程级约束归并：补充默认存储根目录、`-c` 可选、`-port` 覆盖、严格 TOML、http(s) BaseURL、owner-only auth 权限和日志敏感信息边界。
- [x] `.codestable/attention.md` 评估：没有新增每个 feature 都会重复踩的本地环境或工具硬约束，不写入 attention。

## 6. requirement 回写

- [x] 方案 frontmatter 指向 `requirement: bot-message-bridge`，该 requirement 已是 `current`。
- [x] 本次新增用户可感的 Runtime config 与日志配置能力，已更新 `.codestable/requirements/bot-message-bridge.md`：补充用户故事、解决方式、边界和 2026-06-10 变更记录。
- [x] `.codestable/requirements/VISION.md` 已同步 pitch，让 requirement 索引能反映 TOML 管理本地运行参数的能力。

## 7. roadmap 回写

- [x] 方案 frontmatter 没有 `roadmap` / `roadmap_item` 字段，本 feature 非 roadmap 起头。
- [x] 不需要更新 `.codestable/roadmap/` 下的 items.yaml 或 roadmap 主文档。

## 8. attention.md 候选盘点

- [x] 本 feature 未暴露需要补入 `.codestable/attention.md` 的内容。

理由：Go 测试、YAML 校验和配置路径规则都已在 feature 产物与 architecture/requirement 中记录；没有发现“下一个 feature 每次启动都必须先知道”的额外环境、代理、命令或路径陷阱。

## 9. 遗留

- 后续优化点：暂无必须跟进的 issue。
- 已知限制：日志大小控制只保留当前文件和 `.1` 单备份；单条日志大于上限时当前文件可能短暂超过上限，但后续写入会再次触发轮转。该行为符合本 feature 的“不做完整日志平台”边界。
- 实现阶段顺手发现：控制台仍会显示 APIToken 和收到的消息内容，这是既有交互行为，且不因本 feature 写入日志文件；若后续要统一输出与脱敏策略，应另走 `cs-refactor` 或独立 feature。
- 用户终审：已确认。
