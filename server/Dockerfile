# Image for the Go API and the load balancer: both binaries, one image.
# compose.yaml runs /app/api for the API instances and /app/lb for the
# load balancer.

FROM golang:1.27-alpine AS build
WORKDIR /src

# Dependencies first, so editing code doesn't re-download them.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# CGO off gives fully static binaries that run in the tiny image below.
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api . \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/lb ./lb

# Runtime: no shell, no package manager, not root. It has the CA
# certificates the API needs to call Discord over HTTPS.
FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/api /out/lb /app/
USER nonroot
EXPOSE 8080 9000
ENTRYPOINT ["/app/api"]
