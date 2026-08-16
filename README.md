# SearchTerm

磁力聚合搜索工具：聚合多个磁力站点搜索，支持 115 网盘离线下载、Telegram Bot、Android App 和 Docker 部署。

## 功能

- 聚合搜索：观影站、Nyaa、Sukebei、BT1207 等磁力站点
- 流式结果：搜索到结果后依次显示，支持去重、站点筛选、排序和分页
- 115 离线下载：网页端、Telegram Bot、Android App 均可添加磁力或 ed2k 链接
- Telegram Bot：支持关键词搜索，也可直接发送磁力 / ed2k 链接
- Android App：内置独立服务，无需外部服务器；BT1207 Cookie 自动抓取并持久化，超过 2 小时自动续期
- Docker 多架构镜像：amd64 + arm64

## 安装

### Android

1. 打开 Releases 下载最新 APK
2. 安装后直接使用，无需注册
3. BT1207 会自动抓取 Cookie，超过 2 小时自动重新抓取

### Docker

```bash
docker run -d --name searchterm \
  -p 8080:8080 -p 8787:8787 \
  -v searchterm-data:/app/data \
  ghcr.io/dannisjay/searchterm:1.7.1
```

启动后访问 `http://服务器IP:8080`，首次启动时设置管理员账号，请及时修改默认密码。

也可以使用 docker compose：

```yaml
services:
  searchterm:
    image: ghcr.io/dannisjay/searchterm:1.7.1
    container_name: searchterm
    restart: unless-stopped
    ports:
      - "8080:8080"
      - "8787:8787"
    volumes:
      - ./data:/app/data
```

## 使用说明

- 后台“站点配置”中开启需要搜索的磁力站点
- 115 离线下载：后台配置 Cookie 或扫码登录，然后选择离线保存目录
- Telegram Bot：后台填写 TG ID 和 Bot Token，向机器人发送关键词即可搜索

## 声明

本仓库仅发布项目介绍与安装说明，源码不公开。
