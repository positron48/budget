# syntax=docker/dockerfile:1

FROM golang:1.24 as builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/budgetd ./cmd/budgetd

FROM gcr.io/distroless/base-debian12
WORKDIR /app
COPY --from=builder /out/budgetd /app/budgetd
ENV GRPC_ADDR=0.0.0.0:8080
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/budgetd"]


