# syntax=docker/dockerfile:1
FROM golang:1.26.11-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Reproducible: no VCS stamp, no build id, no local paths (CI builds twice and compares hashes).
RUN CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags="-s -w -buildid=" -o /out/njiwa-mail ./cmd/njiwa-mail

# One image, four roles: `serve` (API), `worker` (dispatch + webhooks),
# `inbound` (private SMTP listener + parser) and `scheduler` (periodic jobs).
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 dmail \
 && mkdir -p /var/lib/njiwa-mail && chown 10001:10001 /var/lib/njiwa-mail
COPY --from=build /out/njiwa-mail /usr/local/bin/njiwa-mail
COPY --from=build /out/njiwa-mail /usr/local/bin/dmctl
USER 10001
VOLUME /var/lib/njiwa-mail
EXPOSE 8080 2525
ENTRYPOINT ["njiwa-mail"]
CMD ["serve"]
