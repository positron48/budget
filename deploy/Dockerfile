FROM --platform=$BUILDPLATFORM golang:1.26.9 AS builder
ARG TARGETOS
ARG TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN for command in budgetd marketdata investmentnav investcbr investfxpolicy investalphafx alphafx portfoliohistory; do \
      CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/$command ./cmd/$command || exit 1; \
    done

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /out/ /app/
ENV GRPC_ADDR=0.0.0.0:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/budgetd"]
