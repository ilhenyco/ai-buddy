FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mock-access ./cmd/mock-access

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
# the same image runs the demo access service: entrypoint /mock-access (see docker-compose.yml)
COPY --from=build /out/mock-access /mock-access
COPY --from=build /usr/local/go/lib/time/zoneinfo.zip /zoneinfo.zip
ENV ZONEINFO=/zoneinfo.zip
EXPOSE 8080
ENTRYPOINT ["/api"]
