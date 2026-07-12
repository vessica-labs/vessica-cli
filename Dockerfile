FROM node:24-bookworm-slim AS dashboard

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

FROM golang:1.25-bookworm AS build

ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=dashboard /src/internal/dashboard/assets ./internal/dashboard/assets
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags="-s -w -X github.com/vessica-labs/vessica-cli/internal/version.Version=${VERSION}" \
    -o /out/ves ./cmd/ves

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends bash ca-certificates curl openssh-client \
    && rm -rf /var/lib/apt/lists/* \
    && curl -fsSL https://railway.com/install.sh | bash

ENV PATH="/root/.railway/bin:$PATH"
WORKDIR /var/lib/vessica
COPY --from=build /out/ves /usr/local/bin/ves

EXPOSE 8080
ENTRYPOINT ["ves"]
CMD ["control-plane", "serve"]
