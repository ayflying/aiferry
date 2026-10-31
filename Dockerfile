# 前端独立发布：沿用已确认正常的 0.6.63 后端与迁移文件，不重新编译后端。
# 摘要来自线上正在运行的稳定镜像，防止可变标签带入其他版本。
FROM node:22-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM ghcr.io/ayflying/aiferry:0.6.63@sha256:9d349deb724a9222390f971ab05774b6c8a8bdd19f7cb8096b3ad894f6dd6e4b
ARG VERSION
ARG VCS_REF
LABEL org.opencontainers.image.version="$VERSION" \
      org.opencontainers.image.revision="$VCS_REF" \
      cn.yunloli.aiferry.backend-version="0.6.63" \
      cn.yunloli.aiferry.release-scope="frontend-only"
# 只覆盖静态资源。保留稳定后端二进制、VERSION 与 manifest 目录原样，
# 因此 /healthz 仍如实报告后端 0.6.63，前端发布版本由镜像标签/标签元数据核验。
COPY --from=frontend-build /src/frontend/dist /app/web
