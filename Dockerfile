FROM golang:latest as builder

WORKDIR /go/src/github.com/mdmdirector/mdmdirector/

ENV CGO_ENABLED=0 \
    GOARCH=amd64 \
    GOOS=linux

COPY . .

RUN make deps
RUN make


FROM alpine:latest

RUN apk --update add ca-certificates

COPY --from=builder /go/src/github.com/mdmdirector/mdmdirector/build/linux/mdmdirector /usr/bin/mdmdirector

EXPOSE 8000
CMD ["/usr/bin/mdmdirector"]
