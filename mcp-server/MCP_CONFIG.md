# 使用 uv 运行 WeKnora MCP 服务器

## 多人独立 Key（HTTP/SSE）

远程 MCP 服务可直接复用 WeKnora 已有的租户 API Key 管理能力。服务端设置：

```bash
export MCP_TRANSPORT=http
export MCP_AUTH_MODE=weknora_api_key
export WEKNORA_BASE_URL=http://localhost:8080/api/v1
```

管理员在 WeKnora「设置 → API Keys」中为每位使用者分别创建 Key，并按需配置
capabilities 与 `knowledge_base_ids` 白名单。每位 MCP 客户端使用自己的 Key：

```http
Authorization: Bearer <个人租户 API Key>
```

MCP Server 会按请求隔离并转发该 Key 为 WeKnora REST API 的 `X-API-Key`；Key 的
撤销、过期、能力和知识库范围均由 WeKnora 后端统一校验。原有部署无需修改，默认
仍为 `MCP_AUTH_MODE=shared`，继续使用 `MCP_SERVER_AUTH_TOKEN` +
`WEKNORA_API_KEY`。

> 更推荐使用`uv`来运行基于python的MCP服务。
>
> 也可通过 PyPI 安装：`pip install tencent-weknora-mcp`，或使用 `uvx --from tencent-weknora-mcp weknora-mcp-server`（官方包名 `tencent-weknora-mcp`，由 [Tencent/WeKnora](https://github.com/Tencent/WeKnora) 维护）。

## 1. 安装 uv

```bash
# macOS/Linux
curl -LsSf https://astral.sh/uv/install.sh | sh

# 或使用 Homebrew (macOS)
brew install uv

# Windows
powershell -ExecutionPolicy ByPass -c "irm https://astral.sh/uv/install.ps1 | iex"
```

## 2. MCP 客户端配置

### Claude Desktop 配置

在 Claude Desktop 设置中添加:

```json
{
  "mcpServers": {
    "weknora": {
      "args": [
        "--directory",
        "/path/WeKnora/mcp-server",
        "run",
        "run_server.py"
      ],
      "command": "uv",
      "env": {
        "WEKNORA_API_KEY": "your_api_key_here",
        "WEKNORA_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```

### Cursor 配置

在 Cursor 中，编辑 MCP 配置文件 (通常在 `~/.cursor/mcp-config.json`):

```json
{
  "mcpServers": {
    "weknora": {
      "command": "uv",
      "args": [
        "--directory",
        "/path/WeKnora/mcp-server",
        "run",
        "run_server.py"
      ],
      "env": {
        "WEKNORA_API_KEY": "your_api_key_here",
        "WEKNORA_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```

### KiloCode 配置

对于 KiloCode 或其他支持 MCP 的编辑器，配置如下:

```json
{
  "mcpServers": {
    "weknora": {
      "command": "uv",
      "args": [
        "--directory",
        "/path/WeKnora/mcp-server",
        "run",
        "run_server.py"
      ],
      "env": {
        "WEKNORA_API_KEY": "your_api_key_here",
        "WEKNORA_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```

### 其他 MCP 客户端

对于一般 MCP 客户端配置:

```json
{
  "mcpServers": {
    "weknora": {
      "command": "uv",
      "args": [
        "--directory",
        "/path/WeKnora/mcp-server",
        "run",
        "run_server.py"
      ],
      "env": {
        "WEKNORA_API_KEY": "your_api_key_here",
        "WEKNORA_BASE_URL": "http://localhost:8080/api/v1"
      }
    }
  }
}
```
