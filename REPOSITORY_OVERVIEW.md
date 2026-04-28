# go-cidr-api

基于 Go 的中国省市 CIDR 查询 API。镜像内置 `china_city_cidrs.compact.json` 数据文件，启动后提供 HTTP JSON API，可按省、市查询 IPv4 / IPv6 CIDR。

## 快速开始

默认服务端口为 `30662`。

```bash
docker run --rm \
  -p 30662:30662 \
  kcilnk/go-cidr-api:latest
```

健康检查：

```bash
curl http://127.0.0.1:30662/healthz
```

## Docker Compose

保存为 `docker-compose.yml`：

```yaml
services:
  go-cidr-api:
    image: kcilnk/go-cidr-api:latest
    container_name: go-cidr-api
    restart: unless-stopped
    ports:
      - "30662:30662"
    environment:
      RUN_MODE: http
      ADDR: ":30662"
      DATA_FILE: /app/china_city_cidrs.compact.json
```

启动：

```bash
docker compose up -d
```

查看日志：

```bash
docker compose logs -f go-cidr-api
```

停止：

```bash
docker compose down
```

## 常用接口

查询省列表：

```bash
curl http://127.0.0.1:30662/api/v1/provinces
```

查询城市列表：

```bash
curl "http://127.0.0.1:30662/api/v1/provinces/广东/cities"
```

按省查询 CIDR：

```bash
curl "http://127.0.0.1:30662/api/v1/provinces/广东/cidrs?ip_version=4"
```

按省、市查询 CIDR：

```bash
curl "http://127.0.0.1:30662/api/v1/provinces/广东/cities/深圳/cidrs?ip_version=4"
```

也可以使用查询参数：

```bash
curl "http://127.0.0.1:30662/api/v1/cidrs?province=广东&city=深圳&ip_version=4"
```

`ip_version` 支持：

- `4`
- `6`
- 不传或 `all`，返回 IPv4 和 IPv6 分组

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `RUN_MODE` | `http` | 运行模式，Docker 场景使用 `http` |
| `ADDR` | `:30662` | HTTP 监听地址 |
| `DATA_FILE` | `/app/china_city_cidrs.compact.json` | CIDR 数据文件路径 |

## 镜像标签

推荐生产环境固定版本号：

```yaml
image: kcilnk/go-cidr-api:0.1.0
```

如果希望始终使用最新版本：

```yaml
image: kcilnk/go-cidr-api:latest
```

镜像发布时会同时提供多架构 manifest，支持：

- `linux/amd64`
- `linux/arm64`
