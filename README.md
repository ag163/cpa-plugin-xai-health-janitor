# XAI Health Janitor

CLIProxyAPI 插件：定时探测 xAI/Grok 账号健康状态，自动删除 402 / 401 / 403 / 限流账号，并提供可视化管理面板。

## 功能

- 定时扫描 CPA 内所有 `xai` 账号
- 直连上游 `chat/completions` 探测（自动带 `x-grok-client-version`）
- 自动删除：
  - HTTP **402** spending-limit
  - HTTP **403** permission-denied
  - HTTP **401** auth invalid
  - HTTP **429** rate limit
- 可视化面板：
  - 总账号 / 正常 / 异常
  - 402 / 403 / 401+限流 分类统计
  - 可修改轮询间隔、并发、自动删除开关
  - 一键立即扫描

## 面板地址

```text
/v0/resource/plugins/xai-health-janitor/status
```

## 安装（推荐：GitHub Release）

### 1. 下载对应架构 so

- Linux arm64: `xai-health-janitor-linux-arm64.so`
- Linux amd64: `xai-health-janitor-linux-amd64.so`

放到 CPA 插件目录：

```bash
# arm64 示例
mkdir -p plugins/linux/arm64
cp xai-health-janitor-linux-arm64.so plugins/linux/arm64/xai-health-janitor.so
```

> 注意：CPA 通过文件名识别插件 ID，容器内文件名必须是 `xai-health-janitor.so`。

### 2. 配置

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    xai-health-janitor:
      enabled: true
      priority: 1
      interval_seconds: 300
      model: "grok-4.5"
      cli_version: "0.1.220"
      management_base: "http://127.0.0.1:8317"
      management_key: "你的 remote-management.secret-key"
      probe_enabled: true
      auto_delete: true
      dry_run: false
      concurrency: 3
      delete_status_codes: [402, 403, 429]
      providers: ["xai"]
```

### 3. 重启 / 热加载 CPA

确保 plugins 目录已挂载进容器（例如 docker-compose）：

```yaml
volumes:
  - ./plugins:/CLIProxyAPI/plugins
```

## 一键部署脚本（服务器）

```bash
# 从 GitHub Release 安装到 /opt/cliproxyapi
TAG=v0.1.0 bash scripts/install-from-github.sh
```

## 本地构建

```bash
# Linux arm64
cd go
CGO_ENABLED=1 GOOS=linux GOARCH=arm64 CC=aarch64-linux-gnu-gcc \
  go build -buildmode=c-shared -o ../xai-health-janitor-linux-arm64.so .
```

## 配置字段

| 字段 | 默认 | 说明 |
|---|---|---|
| `interval_seconds` | 300 | 扫描间隔，最小 30 |
| `model` | `grok-4.5` | 探测模型 |
| `cli_version` | `0.1.220` | free 通道必需请求头 |
| `management_base` | `http://127.0.0.1:8317` | 删除接口地址 |
| `management_key` | 空 | **删除必须配置** |
| `probe_enabled` | true | 是否真实请求上游 |
| `auto_delete` | true | 是否自动删除 |
| `dry_run` | false | 只报告不删除 |
| `concurrency` | 3 | 并发探测数 |

## 安全说明

- 不要把 `management_key` 提交到仓库
- 插件属于进程内可信代码，只安装你信任的 release
