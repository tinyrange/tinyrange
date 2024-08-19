FROM golang AS builder

WORKDIR /src/tinyrange

COPY go.mod go.sum .

RUN go mod download

ADD ./cmd cmd
ADD ./pkg pkg
ADD ./stdlib stdlib
ADD ./third_party third_party
ADD ./tools tools
ADD ./LICENSE LICENSE
ADD ./main.go main.go
ADD ./build/tinyrange_qemu.star build/tinyrange_qemu.star

COPY pkg/buildinfo/commit.txt pkg/buildinfo/commit.txt

RUN go run ./tools/build.go

FROM alpine:3.20

RUN apk add qemu-system-x86_64 ca-certificates

COPY --from=builder /src/tinyrange/build/tinyrange_qemu.star /tinyrange_qemu.star
COPY --from=builder /src/tinyrange/build/tinyrange /tinyrange

ENTRYPOINT ["/tinyrange"]