FROM golang:1.25.5-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . ./

RUN rm -rf cmd/clawgo/workspace \
    && mkdir -p cmd/clawgo/workspace \
    && cp -a workspace/. cmd/clawgo/workspace/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags="-s -w" -o /out/clawgo ./cmd/clawgo

FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --create-home --shell /bin/sh clawgo

USER clawgo
WORKDIR /home/clawgo

COPY --from=builder /out/clawgo /usr/local/bin/clawgo

ENV CLAWGO_CONFIG=/home/clawgo/.clawgo/config.json

EXPOSE 18790

VOLUME ["/home/clawgo/.clawgo"]

ENTRYPOINT ["/bin/sh", "-c", "if [ ! -f \"$CLAWGO_CONFIG\" ]; then /usr/local/bin/clawgo onboard; fi; exec /usr/local/bin/clawgo gateway run --config \"$CLAWGO_CONFIG\""]
