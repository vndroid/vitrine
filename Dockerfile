# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# .git is not in the build context, pass the commit:
#   --build-arg REVISION=$(git rev-parse HEAD) --build-arg BRANCH=$(git branch --show-current)
ARG VERSION="" REVISION="" BRANCH=""
RUN CGO_ENABLED=0 go build -trimpath -tags netgo -ldflags "-s -w \
      -X main.version=${VERSION} -X main.revision=${REVISION} -X main.branch=${BRANCH} \
      -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/vitrine ./cmd/vitrine

FROM alpine:3
# ffmpeg: video thumbnails; imagemagick + ghostscript: pdf/ps thumbnails,
# imagemagick-heic: AVIF/HEIC thumbnails, imagemagick-jpeg: jpeg output;
# coreutils: "du" for foldersize.type "shell-du"
# uid/gid 1000 matches the first user of most hosts, so mounted config
# files may stay private (0600)
RUN apk add --no-cache ffmpeg imagemagick imagemagick-heic imagemagick-jpeg ghostscript coreutils \
 && addgroup -S -g 1000 vitrine && adduser -S -u 1000 -G vitrine -H vitrine \
 && mkdir -p /share /cache /config && chown vitrine:vitrine /cache
COPY --from=build /out/vitrine /usr/local/bin/vitrine
USER vitrine
ENV VITRINE_ROOT=/share \
    VITRINE_CACHE=/cache \
    VITRINE_LISTEN=:8080
EXPOSE 8080
VOLUME ["/cache"]
ENTRYPOINT ["vitrine"]
