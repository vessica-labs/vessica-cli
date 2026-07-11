FROM golang:1.25-bookworm AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ves ./cmd/ves

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
