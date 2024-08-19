FROM ghcr.io/tinyrange/tinyrange:latest AS builder

WORKDIR /work

COPY ./hello.c hello.c

RUN --mount=target=/root/.cache/tinyrange/build,type=cache \
    /tinyrange login \
    build-base \
    -f /work/hello.c \
    -E "source /etc/profile;gcc -static -o /root/hello /root/hello.c" \
    -o /root/hello

RUN chmod +x /work/hello

FROM scratch

COPY --from=builder /work/hello /hello

ENTRYPOINT ["/hello"]