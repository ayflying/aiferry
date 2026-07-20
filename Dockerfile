FROM node:22-alpine AS frontend-build
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-alpine AS backend-build
RUN apk add --no-cache git
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /out/aiferry ./main.go

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
COPY --from=backend-build /src/manifest /app/manifest
COPY --from=frontend-build /src/frontend/dist /app/web
ENV TZ=Asia/Shanghai \
    WEB_ROOT=/app/web
EXPOSE 8080
ENTRYPOINT ["/app/aiferry"]
