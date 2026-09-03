### Xiaoya BYOA production image
### Keep Xiaoya public catalog/update runtime, replace only the core binary with this fork.
FROM alpine:edge AS builder
LABEL stage=go-builder
WORKDIR /app/
RUN apk add --no-cache bash curl jq gcc git go musl-dev
COPY go.mod go.sum ./
RUN go mod download
COPY ./ ./
RUN bash build.sh release docker

FROM xiaoyaliu/alist:latest
LABEL MAINTAINER="xiaoya-byoa"
ARG INSTALL_ARIA2=false
ARG USER=alist
ARG UID=1001
ARG GID=1001

WORKDIR /opt/alist/

RUN addgroup -g ${GID} ${USER} && \
    adduser -D -u ${UID} -G ${USER} ${USER} && \
    mkdir -p /opt/alist/data

RUN apk add --no-cache tzdata

# PowerList/OpenList fork core overlays Xiaoya's original alist binary.
COPY --from=builder --chmod=755 --chown=${UID}:${GID} /app/bin/alist ./alist
COPY --chmod=755 --chown=${UID}:${GID} scripts/byoa-xiaoya-bootstrap.sh /byoa-xiaoya-bootstrap.sh
COPY --chmod=755 --chown=${UID}:${GID} scripts/byoa-xiaoya-normalize.sh /byoa-xiaoya-normalize.sh
COPY --chmod=755 --chown=${UID}:${GID} entrypoint.sh /entrypoint.sh

RUN /entrypoint.sh version

ENV UMASK=022 RUN_ARIA2=${INSTALL_ARIA2}
VOLUME /opt/alist/data/
EXPOSE 5244 5245

# 明确覆盖 Xiaoya 基础镜像入口，确保 BYOA bootstrap/normalize 由同一 PID 1 控制。
ENTRYPOINT ["/entrypoint.sh"]
