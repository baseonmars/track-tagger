# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.27.2-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /track-tagger ./cmd/track-tagger

FROM scratch
COPY --from=build /track-tagger /track-tagger
ENV XDG_CONFIG_HOME=/state
USER 65532:65532
ENTRYPOINT ["/track-tagger"]
CMD ["scan", "/music"]
