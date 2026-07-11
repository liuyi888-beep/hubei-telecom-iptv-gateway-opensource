FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -tags netgo,osusergo -trimpath -ldflags="-s -w" -o /out/iptv-gateway-go .

FROM scratch
WORKDIR /app
COPY --from=build /out/iptv-gateway-go /app/iptv-gateway-go
VOLUME ["/app/data"]
EXPOSE 8899/tcp 8555/tcp
ENTRYPOINT ["/app/iptv-gateway-go", "-config", "/app/config/config.json", "-data-dir", "/app/data"]
