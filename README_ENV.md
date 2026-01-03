# Refactor 环境与凭证

## 凭证文件

`refactor/internal/credential/store.go` 默认从 `DATA_DIR/accounts.json` 读取。

本仓库已将根目录 `accounts.json` 复制到：

- `refactor/data/accounts.json`

## 环境变量

建议在 `refactor/` 下启动，默认读取 `refactor/.env`：

- `DATA_DIR=./data`
- `API_USER_AGENT=antigravity/1.11.9 windows/amd64`（Claude 模型必须以 `antigravity/` 开头）
- `ENDPOINT_MODE=production`

启动示例：

```bash
cd refactor
set -a
. ./.env
set +a
go run ./cmd/server
```

