# SearchTerm

磁力聚合搜索（Go + Web + Telegram Bot），支持 Docker 和单二进制两种部署方式。

## 功能

- 聚合多个磁力站点搜索，结果按 infohash 去重，按大小从大到小排序
- 结果展示来源站点标签、磁力链接、文件大小、一键复制磁链
- 后台登录后可配置站点账号、TG 用户、TG Bot、115 授权、背景图
- 管理站点采用统一列表：观影站 / 七味 / Nyaa / Sukebei 从上到下单独配置，每个站点打钩开启、取消勾选关闭
- 站点默认开启七味和 Nyaa，Sukebei 默认关闭；开启哪个站点就搜索哪个站点
- 观影站（gying）适配器：纯 Go 求解 PoW 挑战、账号登录、搜索、详情
- Nyaa / Sukebei 适配器：无需账号，RSS 聚合搜索
- 七味（qiwei）适配器：无需账号，站点搜索
- 网页端结果 10 条一页，TG 机器人结果最多 30 条、6 条一页，均带翻页和跳页
- 网页端与 TG 机器人均可添加到 115 网盘离线下载，可选最多 6 个保存目录
- 管理后台支持修改账号密码、上传头像，右上角头像可直达账号设置
- 管理后台可配置背景图 URL，登录页、首页和后台全局生效

## Docker 部署（推荐）

只需要两个东西：

1. `docker-compose.yml`
2. `data` 文件夹（可空，用于持久化数据库、密钥和磁力日志）

示例目录：

```text
searchterm/
├── docker-compose.yml
└── data/
```

`docker-compose.yml`：

```yaml
services:
  searchterm:
    image: ghcr.io/dannisjay/searchterm:1.9.0
    container_name: searchterm
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
```

启动：

```bash
docker compose up -d
```

容器启动时会自动生成 `config.json`（默认监听 8080、数据目录 /app/data），不需要手动创建。首次访问 http://localhost:8080 使用默认账号 `admin / change-me` 登录，登录后建议在账号设置里修改密码。

常用配置可通过环境变量覆盖：

```yaml
    environment:
      - SEARCHTERM_LISTEN=:8080
      - SEARCHTERM_DATA_DIR=/app/data
      - SEARCHTERM_ADMIN_USER=admin
      - SEARCHTERM_ADMIN_PASSWORD=change-me
```

支持的环境变量：

- `SEARCHTERM_LISTEN`
- `SEARCHTERM_DATA_DIR`
- `SEARCHTERM_ADMIN_USER`
- `SEARCHTERM_ADMIN_PASSWORD`
- `SEARCHTERM_SECRET_KEY`
- `SEARCHTERM_PUBLIC_URL`
- `SEARCHTERM_MAGNET_LOG_DIR`
- `SEARCHTERM_MAGNET_LOG_MAX_FILE_MB`
- `SEARCHTERM_MAGNET_LOG_MAX_FILES`

## 单二进制部署

```bash
go build -o searchterm ./cmd/server
./searchterm
```

默认从当前目录读取 `config.json`，不存在则自动生成。

## 开发

```bash
go run ./cmd/server
```

打开 http://localhost:8080 使用管理员账号登录。

## Telegram

1. 在 BotFather 创建机器人，获取 Bot Token
2. 后台管理 -> TG Bot，填写 TG ID（多个用分号隔开）和 Bot Token，点击保存
3. 直接向机器人发送关键词即可搜索

## 115 离线下载

1. 后台管理 -> 115授权，登录方式选 Cookie 时直接粘贴 Cookie；或选二维码，用 115 App 扫码后获取 Token，选择设备并保存，系统会自动换成对应设备的 Cookie
2. 打开页面会自动校验 Cookie 有效性，已选离线目录会以悬浮标签展示
3. 点击“配置离线目录”弹出 115 文件列表，最多选择 6 个保存位置，已选项可单独取消
4. 网页搜索卡片点击“添加到115网盘”后选择目录；TG 机器人同样先选择目录再离线

## 构建

```bash
# 本机
go build -o searchterm ./cmd/server

# Docker
docker compose up -d
```

## 授权说明

观影站适配器逻辑参考自 [fish2018/pansou](https://github.com/fish2018/pansou)（MIT License），已在源码中保留版权声明。
115 离线下载接口参考自 [ChenyangGao/p115client](https://github.com/ChenyangGao/p115client)（MIT License），已在源码中保留版权声明。