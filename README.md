# SearchTerm

磁力聚合搜索工具：聚合多个磁力站点搜索，支持 115 网盘离线下载、Telegram Bot、Android App 和 Docker 部署。

## 功能

- 聚合搜索：观影站、七味、Nyaa、Sukebei 等磁力站点
- 流式结果：搜索到结果后依次显示，支持去重、站点筛选、排序和分页
- 115 离线下载：网页端、Telegram Bot、Android App 均可添加磁力或 ed2k 链接
- Telegram Bot：支持关键词搜索，也可直接发送磁力 / ed2k 链接
- Android App：内置独立服务，无需外部服务器
- Docker 多架构镜像：amd64 + arm64

## 安装

### Android

1. 打开 Releases 下载最新 APK
2. 安装后直接使用，无需注册

### Docker

只需要 `docker-compose.yml` 和一个 `data` 文件夹：

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

容器启动时会自动生成 `config.json`，无需手动创建。项目没有默认账号/密码；首次访问 `http://服务器IP:8080` 时，请按页面提示创建管理员账号和密码。

## 使用说明

- 后台“站点配置”中开启需要搜索的磁力站点
- 115 离线下载：后台配置 Cookie 或扫码登录，然后选择离线保存目录
- Telegram Bot：后台填写 TG ID 和 Bot Token，向机器人发送关键词即可搜索

## 源码

Docker 版源码位于 `versions/docker/1.9.0`。
