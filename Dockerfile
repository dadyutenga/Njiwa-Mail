# syntax=docker/dockerfile:1
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ditronics-mail ./cmd/ditronics-mail

# One image, two roles: `serve` (API) and `worker` (dispatch + retention).
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 dmail
COPY --from=build /out/ditronics-mail /usr/local/bin/ditronics-mail
USER 10001
EXPOSE 8080
ENTRYPOINT ["ditronics-mail"]
CMD ["serve"]
