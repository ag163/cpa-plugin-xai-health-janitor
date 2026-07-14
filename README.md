# XAI Health Janitor

CLIProxyAPI 插件：只读取真实 xAI 请求已写入的账号状态，清理确认失效的 401 / 402 / 403 账号；绝不主动请求 xAI。

## 功能

- 没有近期真实 xAI 用户流量时完全闲置
- 定时读取 CPA 内所有 `xai` 账号的 `status` / `status_message`
- 仅在连续两次出现明确硬失败时自动删除：
  - HTTP **402** spending-limit
  - HTTP **403** permission-denied
  - HTTP **401** auth invalid
- HTTP **429**、`rate limit`、`usage exhausted` 仅保留和展示，绝不删除
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
      interval_seconds: 600
      model: "grok-4.5"
      cli_version: "0.1.220"
      management_base: "http://127.0.0.1:8317"
      management_key: "你的 remote-management.secret-key"
      # Legacy field. The plugin never performs upstream probes.
      probe_enabled: false
      auto_delete: true
      dry_run: false
      providers: ["xai"]
      require_user_traffic: true
      hard_failure_confirmations: 2
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

`deploy-on-server.sh` 需要从服务器环境传入管理密钥，绝不在仓库中保存：

```bash
CPA_MANAGEMENT_KEY='your-remote-management-plaintext-key' bash deploy-on-server.sh
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
| `interval_seconds` | 600 | 本地状态检查间隔，最小 30 |
| `management_base` | `http://127.0.0.1:8317` | 删除接口地址 |
| `management_key` | 空 | **删除必须配置** |
| `probe_enabled` | false | 兼容旧配置；主动上游探测已被永久禁用 |
| `auto_delete` | true | 是否自动删除已确认的硬失败账号 |
| `dry_run` | false | 只报告不删除 |
| `require_user_traffic` | true | 无真实 xAI 用户流量时完全闲置 |
| `hard_failure_confirmations` | 2 | 删除前所需的不同真实失败事件数（以 CPA `updated_at` 区分） |

## 安全说明

- 不要把 `management_key` 提交到仓库
- 插件属于进程内可信代码，只安装你信任的 release
- 插件不会调用 xAI 上游接口，因此不会因健康检查增加出口 IP 风控压力
