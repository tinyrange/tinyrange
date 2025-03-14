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

RUN go run ./tools/build.go

FROM alpine:3.20

RUN apk add ca-certificates

RUN mkdir /tinyqemu

COPY --from=builder /src/tinyrange/build/qemu-system-x86_64 /tinyqemu/
COPY --from=builder /src/tinyrange/build/tinyrange_qemu /tinyqemu/
COPY --from=builder /src/tinyrange/build/tinyrange /tinyrange

ENTRYPOINT ["/tinyrange"]
