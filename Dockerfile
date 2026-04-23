FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o app ./backend/cmd

FROM alpine:3.21
WORKDIR /root
RUN mkdir -p /data
ENV APP_ADDR=:8080
ENV DATABASE_PATH=/data/reminder.db
COPY --from=builder /app/app .
COPY --from=builder /app/web ./web
EXPOSE 8080
VOLUME ["/data"]
CMD ["./app"]
