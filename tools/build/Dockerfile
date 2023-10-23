# Step 1: Prepare modules
FROM golang:1.20 as builder
ARG TARGETOS
ARG TARGETARCH
ARG VERSION_ID

COPY go.mod go.sum /app/
#COPY#

WORKDIR /app
COPY . .
#MOD_PREPARE#
RUN go mod download
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -ldflags="-X github.com/tiny-systems/module/cli.versionID=${VERSION_ID}" -o /bin/app #MAIN_PATH#

# Step 3: Final
FROM gcr.io/distroless/static:nonroot
COPY --from=builder /bin/app /app
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
USER 65532:65532
CMD ["/app", "run"]
