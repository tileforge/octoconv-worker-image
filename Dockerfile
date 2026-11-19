FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/image-worker ./cmd/image-worker

FROM debian:bookworm-slim
RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      ca-certificates \
      libvips-tools \
 && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/image-worker /usr/local/bin/image-worker
USER nobody
ENTRYPOINT ["/usr/local/bin/image-worker"]
