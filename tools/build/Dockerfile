# Step 1: Prepare modules
FROM golang:1.20.3-alpine3.17 as builder
COPY go.mod go.sum /app/
#COPY#

WORKDIR /app
COPY . .
#MOD_PREPARE#
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-X github.com/tiny-systems/module/cli.versionID=#VERSION_ID#" -o /bin/app #MAIN_PATH#

# Step 3: Final
FROM scratch
COPY --from=builder /bin/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
CMD ["/app", "run"]
