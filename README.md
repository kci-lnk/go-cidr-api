# go-cidr-api

基于 Go 的中国大陆省市及运营商 CIDR 查询 API。数据源使用当前目录下的 `china_city_cidrs.compact.json`，可从 ip2region XDB 数据库重新生成。部署方式调整为：

- 腾讯云 Web 函数
- 通过函数 URL 直接触发
- 本地与云上共用同一套 HTTP 服务入口

这次不再走 API 网关事件 / SCF 事件对象，而是直接启动 HTTP Server，让函数 URL 直接透传 HTTP 请求。

## 为什么改成函数 URL

根据腾讯云官方文档：

- 函数 URL 是函数的专用 HTTP(S) 端点，可直接被浏览器、curl、Postman 调用
- 对于 Web 函数，HTTP 请求会直接透传，不再做 event 格式转换
- Web 函数需要提供 `scf_bootstrap` 启动文件，并监听 `9000` 端口

参考文档：

- [函数 URL 概述](https://cloud.tencent.com/document/product/583/96099)
- [启动文件说明](https://cloud.tencent.com/document/product/583/56126)

## 当前 API

所有接口均为 `GET`，返回 JSON。

如果客户端携带 `Accept-Encoding: gzip` 或 `Accept-Encoding: br`，服务会自动返回对应的压缩响应，并带上 `Content-Encoding` / `Vary: Accept-Encoding` 头。

### 1. 健康检查

```bash
curl http://127.0.0.1:8080/healthz
```

### 2. 查询省列表

```bash
curl http://127.0.0.1:8080/api/v1/provinces
```

### 3. 根据省查询城市列表

```bash
curl "http://127.0.0.1:8080/api/v1/provinces/广东/cities"
```

### 4. 根据省、市、运营商查询 CIDR

路径风格：

```bash
curl "http://127.0.0.1:8080/api/v1/provinces/广东/cities/深圳/cidrs?ip_version=4"
curl "http://127.0.0.1:8080/api/v1/provinces/河北/cities/石家庄/cidrs?operator=移动&ip_version=4"
```

查询参数风格：

```bash
curl "http://127.0.0.1:8080/api/v1/cidrs?province=广东&city=深圳&ip_version=4"
curl "http://127.0.0.1:8080/api/v1/cidrs?province=河北&city=石家庄&operator=移动&ip_version=4"
```

### 5. 只传省份，查询全省 CIDR

路径风格：

```bash
curl "http://127.0.0.1:8080/api/v1/provinces/甘肃/cidrs?ip_version=4"
```

查询参数风格：

```bash
curl "http://127.0.0.1:8080/api/v1/cidrs?province=甘肃&ip_version=4"
curl "http://127.0.0.1:8080/api/v1/cidrs?province=河北&operator=电信&ip_version=4"
```

### 6. 使用 selector 查询

`selector` 可以只传城市，也可以在城市名后追加 `电信`、`联通` 或 `移动`：

```bash
curl "http://127.0.0.1:8080/api/v1/cidrs?selector=石家庄&ip_version=4"
curl "http://127.0.0.1:8080/api/v1/cidrs?selector=石家庄移动&ip_version=4"
curl "http://127.0.0.1:8080/api/v1/cidrs?selector=北京电信&ip_version=6"
```

`selector` 不能和 `province`、`city`、`operator` 同时使用，但可以和 `ip_version` 一起使用。当前数据没有重名城市；如果后续 XDB 出现重名城市，selector 查询会返回 HTTP 409，并提示候选省市。

运营商参数只接受 `电信`、`联通`、`移动`。不传运营商时仍返回该城市或省份的全部 CIDR。

## 名称规则

返回结果中的省市名称会自动去掉行政后缀，例如：

- `广西壮族自治区 -> 广西`
- `新疆维吾尔自治区 -> 新疆`
- `北京市 -> 北京`
- `阿坝藏族自治州 -> 阿坝`

查询时同时支持带后缀和不带后缀的写法，例如：

- `广东` 和 `广东省`
- `北京` 和 `北京市`
- `深圳` 和 `深圳市`

## 从 XDB 生成数据

将 XDB 文件放在本仓库已忽略的 `ipdata/` 目录：

```text
ipdata/base_full_v4.xdb
ipdata/base_full_v6.xdb
```

然后运行：

```bash
task data:generate
```

也可以指定其他输入目录或输出文件：

```bash
task data:generate IPDATA_DIR=/path/to/ipdata
task data:generate IPDATA_DIR=/path/to/ipdata OUTPUT=/tmp/china-city-cidrs.json
```

生成器会校验 XDB Header，流式读取 IPv4/IPv6 segment index，仅保留中国大陆 31 个省级行政区，并以临时文件加原子重命名的方式更新输出文件。城市节点保留原有的 `4`、`6` 字段，并增加可选的运营商数据：

```json
{
  "河北省": {
    "石家庄": {
      "4": ["1.2.3.0/24"],
      "6": ["2001:db8::/48"],
      "operators": {
        "电信": {"4": ["1.2.3.0/25"]},
        "联通": {"4": ["1.2.3.128/26"]},
        "移动": {"6": ["2001:db8::/49"]}
      }
    }
  }
}
```

原始 ISP 字段按 `/` 拆分。`电信/中国电信`、`联通/中国联通`、`移动/中国移动/铁通/中移铁通` 会归一到三个运营商；一个混合 ISP 网段可以同时进入多个运营商集合，未匹配的 ISP 仍保留在城市总集合中。

## 本地调试

### 方式一：普通本地 HTTP 调试

```bash
go run . -mode=http -addr=:8080
```

或者：

```bash
task run:http
task run:http ADDR=:18080
```

### 方式二：按 Web 函数方式本地模拟

这个模式会优先读取 `PORT` 环境变量，和腾讯云 Web 函数更一致。

```bash
PORT=9000 go run . -mode=web
```

或者：

```bash
task run:web
task run:web WEB_PORT=9001
```

## 腾讯云部署

### 1. 打包

```bash
task build
```

或：

```bash
./scripts/build-web.sh
```

会生成：

```text
dist/cidr-api-function-url.zip
```

ZIP 内包含：

- `cidr-api`
- `scf_bootstrap`
- `china_city_cidrs.compact.json`

### 2. 创建云函数

建议配置：

- 函数类型：Web 函数
- 运行环境：Golang
- 启动文件：项目内的 `scf_bootstrap`

`scf_bootstrap` 已在项目根目录提供，内容见 [scf_bootstrap](/Volumes/PSSD/Local/go-cidr-api/scf_bootstrap)。

### 3. 开启函数 URL

在函数创建完成后，为该 Web 函数开启函数 URL，之后可以直接通过函数 URL 访问：

```text
https://<app-id>-<url-id>.<region>.tencentscf.com
```

然后直接访问：

```bash
curl "https://<your-function-url>/api/v1/cidrs?province=甘肃&ip_version=4"
```

## Docker 发布

Docker 版本号固定写在根目录 `VERSION`，例如：

```text
0.1.0
```

Docker Hub 仓库配置在 `deploy/docker/.env.example`，本地如果要覆盖，可以新建 `deploy/docker/.env`：

```bash
CIDR_API_DOCKER_IMAGE_REPO=kcilnk/go-cidr-api
CIDR_API_DOCKER_PLATFORMS=linux/amd64,linux/arm64
CIDR_API_DOCKER_PORT=30662
```

本地构建当前架构镜像：

```bash
task docker:build
```

本地运行镜像，默认对外暴露 `30662`：

```bash
task docker:run
curl http://127.0.0.1:30662/healthz
```

发布到 Docker Hub：

```bash
task docker:publish
```

也可以临时指定仓库：

```bash
task docker:publish DOCKER_IMAGE_REPO=your-dockerhub-user/go-cidr-api
```

发布命令会使用 `docker buildx` 多阶段构建并复用 `~/.cache/go-cidr-api-buildx/<arch>` 缓存，推送：

- `kcilnk/go-cidr-api:<VERSION>-amd64`
- `kcilnk/go-cidr-api:<VERSION>-arm64`
- `kcilnk/go-cidr-api:<VERSION>`
- `kcilnk/go-cidr-api:latest`

其中 `<VERSION>` 来自根目录 `VERSION`，`latest` 会自动更新到同一组多架构镜像。

## Task 命令

```bash
task --list
task tidy
task fmt
task test
task check
task data:generate
task run:http
task run:web
task build
task docker:version
task docker:build
task docker:run
task docker:publish
task clean
```

## 测试

```bash
go test ./...
```
