# Attention

本文件是 CodeStable 技能启动必读的项目注意事项入口。所有 CodeStable 子技能开始工作前必须读取它。

## 项目碎片知识

<!-- cs-note managed: 用 cs-note 维护，新条目按下面分节追加 -->

### 编译与构建

### 运行与本地起服务

### 测试

### 命令与脚本陷阱

- 后续改启动参数、默认路径、构建方式或 Linux 运行方式时，要同步检查是否需要更新 `scripts/linux-service.sh` 和 `docs/user/linux-systemd-deploy.md`。
- `.codestable/tools/validate-yaml.py` 必须用 `python3 .codestable/tools/validate-yaml.py --file <path>` 调用；脚本本身没有执行权限，不能直接运行。

### 路径与目录约定

### 环境变量与凭证

- OpenTelemetry OTLP exporter 默认会读取 `OTEL_EXPORTER_OTLP*`；本项目 telemetry 配置必须保持 TOML-only，构造 exporter 时要屏蔽这些环境变量，不能把它们作为第二配置源。

### 其他
