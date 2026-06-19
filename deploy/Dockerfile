# syntax=docker/dockerfile:1

# --- build stage: static, CGO-free binary ---
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
ARG COMMIT=none
ARG DATE=unknown
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
      -X github.com/snangue/robin/internal/version.Version=${VERSION} \
      -X github.com/snangue/robin/internal/version.Commit=${COMMIT} \
      -X github.com/snangue/robin/internal/version.Date=${DATE}" \
    -o /out/robin ./cmd/robin

# --- runtime stage: distroless static, non-root ---
# distroless/static ships CA roots and /etc/passwd; the binary is fully static.
FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/robin /robin
USER nonroot:nonroot
# 4000 = proxy plane (bind loopback in a sidecar); 4001 = admin plane (probes/metrics).
EXPOSE 4000 4001
ENTRYPOINT ["/robin"]
