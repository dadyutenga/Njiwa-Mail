# syntax=docker/dockerfile:1
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ditronics-mail ./cmd/ditronics-mail

# One image, four roles: `serve` (API), `worker` (dispatch + webhooks),
# `inbound` (private SMTP listener + parser) and `scheduler` (periodic jobs).
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 dmail \
 && mkdir -p /var/lib/ditronics-mail && chown 10001:10001 /var/lib/ditronics-mail
COPY --from=build /out/ditronics-mail /usr/local/bin/ditronics-mail
USER 10001
VOLUME /var/lib/ditronics-mail
EXPOSE 8080 2525
ENTRYPOINT ["ditronics-mail"]
CMD ["serve"]
