FROM golang:1.18-alpine AS builder
RUN apk --no-cache add make git
WORKDIR /go/src/hooks
COPY / ./
RUN make build-perms && ls -lh bin/*

FROM scratch
COPY --from=0 /go/src/hooks/bin/perms /perms
ENTRYPOINT ["/perms"]
