FROM node:24-bookworm-slim@sha256:6f7b03f7c2c8e2e784dcf9295400527b9b1270fd37b7e9a7285cf83b6951452d AS dashboard

WORKDIR /src
COPY web/dashboard/package.json web/dashboard/package-lock.json ./web/dashboard/
RUN cd web/dashboard && npm ci
COPY web/dashboard ./web/dashboard
COPY docs ./docs
COPY internal/dashboard ./internal/dashboard
RUN rm -f internal/dashboard/docs/*.md \
    && cp docs/*.md internal/dashboard/docs/ \
    && cd web/dashboard \
    && npm run generate:api \
    && npm run build

FROM golang:1.25.12-bookworm@sha256:ea341baa9bd5ba6784f6d7161ace70544349a6242d54d34a0fbfd2c4d51c9d58 AS build

ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=dashboard /src/internal/dashboard/assets ./internal/dashboard/assets
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/vessica-labs/vessica-cli/internal/version.Version=${VERSION}" \
    -o /out/ves ./cmd/ves

FROM debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818 AS railway

ARG TARGETARCH
ARG RAILWAY_VERSION=5.26.4
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl \
    && rm -rf /var/lib/apt/lists/* \
    && case "$TARGETARCH" in \
         amd64) railway_target=x86_64-unknown-linux-gnu; railway_sha=b82bbaa4c9dda589360a561d17294add6a9ad70eb94858164befd859c6a9e74c ;; \
         arm64) railway_target=aarch64-unknown-linux-musl; railway_sha=d2f0c0c169bc9f72db81e32a83d0182c4170e1d3c63c97a9ca3a0be8c9cf6425 ;; \
         *) echo "unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
       esac \
    && archive="railway-v${RAILWAY_VERSION}-${railway_target}.tar.gz" \
    && curl -fsSLo "/tmp/${archive}" "https://github.com/railwayapp/cli/releases/download/v${RAILWAY_VERSION}/${archive}" \
    && echo "${railway_sha}  /tmp/${archive}" | sha256sum -c - \
    && tar -xzf "/tmp/${archive}" -C /usr/local/bin railway \
    && rm "/tmp/${archive}"

FROM debian:bookworm-slim@sha256:7b140f374b289a7c2befc338f42ebe6441b7ea838a042bbd5acbfca6ec875818

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates curl git openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --system --uid 10001 --create-home --home-dir /var/lib/vessica --shell /usr/sbin/nologin vessica

COPY --from=railway /usr/local/bin/railway /usr/local/bin/railway
WORKDIR /var/lib/vessica
COPY --from=build /out/ves /usr/local/bin/ves
RUN chown -R vessica:vessica /var/lib/vessica
USER vessica

EXPOSE 8080
ENTRYPOINT ["ves"]
CMD ["control-plane", "serve"]
