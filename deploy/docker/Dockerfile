FROM golang:1.27 AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

ARG TARGET
ARG TARGETARCH
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/app ./${TARGET}

FROM gcr.io/distroless/static-debian13:nonroot
WORKDIR /
COPY --from=build /out/app /app
USER nonroot:nonroot
ENTRYPOINT ["/app"]
