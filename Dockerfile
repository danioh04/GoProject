FROM golang:1.26-alpine AS build
WORKDIR /src
RUN apk update && apk upgrade --no-cache && apk add --no-cache ca-certificates
COPY go.* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /bin/server ./cmd/server

FROM scratch
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=build /bin/server /server
USER 65534:65534
EXPOSE 8080
ENTRYPOINT ["/server"]
