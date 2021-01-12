FROM golang:latest as builder
WORKDIR /builder/
ADD . /builder/
RUN CGO_ENABLED=0 GOOS=linux go build -mod vendor -o app

FROM alpine:latest
RUN apk --no-cache add ca-certificates
WORKDIR /root/
COPY --from=builder /builder/app /root/app
ENTRYPOINT ["./app"]
