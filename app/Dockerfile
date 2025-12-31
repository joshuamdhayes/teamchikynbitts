FROM golang:1.24-alpine AS builder

WORKDIR /app
COPY src/main.go .
RUN go build -o main main.go

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/main .

EXPOSE 8080
CMD ["./main"]
