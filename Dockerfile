FROM alpine

# we need ca-certificates for any external https communication
RUN apk --update upgrade && \
    apk add curl ca-certificates && \
    update-ca-certificates && \
    rm -rf /var/cache/apk/*

ADD ./build/nomad-crashloop-detector-linux-amd64 /nomad-crashloop-detector
ENTRYPOINT ["/nomad-crashloop-detector"]
