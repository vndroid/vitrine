# syntax=docker/dockerfile:1

FROM golang:1.26-alpine3.23 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# .git is not in the build context, pass the commit:
#   --build-arg REVISION=$(git rev-parse HEAD) --build-arg BRANCH=$(git branch --show-current)
ARG VERSION=""
ARG REVISION=""
ARG BRANCH=""
RUN CGO_ENABLED=0 go build -trimpath -tags netgo -ldflags "-s -w \
      -X main.version=${VERSION} -X main.revision=${REVISION} -X main.branch=${BRANCH} \
      -X main.buildDate=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    -o /out/vitrine ./cmd/vitrine

FROM alpine:3.23
# ffmpeg: video thumbnails; imagemagick + ghostscript: pdf/ps thumbnails,
# imagemagick-heic: AVIF/HEIC thumbnails, imagemagick-jpeg: jpeg output;
# coreutils: "du" for foldersize.type "shell-du"
# vitrine runs as uid/gid 1000, which matches the first user of most hosts,
# so mounted config files may stay private (0600). www-data (uid/gid 82,
# the web server user of Alpine) exists too and vitrine is a member of its
# group: files of either owner are readable when they are group or world
# readable, and /cache is writable for both (setgid keeps the group).
# "docker run --user www-data" runs vitrine as that user instead.
RUN apk add --no-cache ffmpeg imagemagick imagemagick-heic imagemagick-jpeg ghostscript coreutils \
 && addgroup -S -g 1000 vitrine && adduser -S -u 1000 -G vitrine -H vitrine \
 && adduser -S -u 82 -G www-data -H www-data \
 && addgroup vitrine www-data \
 && mkdir -p /share /cache /config \
 && chown vitrine:www-data /cache && chmod 2775 /cache
COPY --from=build /out/vitrine /usr/local/bin/vitrine
COPY --chmod=755 docker-entrypoint.sh /usr/local/bin/
USER vitrine
ENV VITRINE_ROOT=/share \
    VITRINE_CACHE=/cache \
    VITRINE_LISTEN=:8080

EXPOSE 8080
VOLUME ["/cache"]
WORKDIR /share

ENTRYPOINT ["docker-entrypoint.sh"]
CMD ["vitrine"]
