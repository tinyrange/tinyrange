FROM ghcr.io/tinyrange/tinyrange:latest AS builder

RUN --mount=target=/root/.cache/tinyrange/build,type=cache \
    /tinyrange login \
    build-base clang ninja libjpeg \
    --write-root /layer.tar

WORKDIR /final

RUN tar xf /layer.tar

FROM scratch

COPY --from=builder /final .

RUN /init -run-scripts /.pkg/scripts.json

ENTRYPOINT ["/bin/sh"]