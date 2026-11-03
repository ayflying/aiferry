# AiFerry 正式发布镜像：完整构建后端、前端与数据库迁移。
#
# 历史教训：本文件在 0.6.72–0.6.81 期间被改成「只替换 /app/web 的前端独立发布」，
# 后端被 FROM 到固定的 0.6.63 镜像上。结果是这期间的每一次发布都没有重新编译后端，
# 线上 /healthz 长期停留在 0.6.63，任何后端修复都无法上线，而且 CI 全绿、完全看不出来。
# 现在恢复为完整构建：后端二进制、VERSION、manifest（迁移）和前端静态资源都由同一次
# 提交构建，版本标签与代码内容严格对应。
FROM node:22-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend-build
ARG VERSION=dev
ARG VCS_REF=unknown
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X github.com/yunloli/aiferry/internal/buildinfo.Version=${VERSION} -X github.com/yunloli/aiferry/internal/buildinfo.Revision=${VCS_REF}" -o /out/aiferry ./main.go

FROM alpine:3.22
ARG VERSION=dev
ARG VCS_REF=unknown
RUN apk add --no-cache ca-certificates tzdata wget
WORKDIR /app
LABEL org.opencontainers.image.title="AiFerry" \
      org.opencontainers.image.description="OpenAI compatible AI gateway" \
      org.opencontainers.image.source="https://github.com/ayflying/aiferry" \
      org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$VCS_REF"
COPY --from=backend-build /out/aiferry /app/aiferry
COPY --from=backend-build /src/VERSION /app/VERSION
COPY --from=backend-build /src/manifest /app/manifest
COPY --from=frontend-build /src/frontend/dist /app/web
ENV TZ=Asia/Shanghai \
    WEB_ROOT=/app/web
EXPOSE 8080
ENTRYPOINT ["/app/aiferry"]
