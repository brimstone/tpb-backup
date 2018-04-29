FROM golang:1.10-alpine as builder

RUN apk add -U gcc musl-dev file

COPY . /go/src/github.com/brimstone/tpb-backup/

WORKDIR /go/src/github.com/brimstone/tpb-backup/

ARG GOARCH=amd64
ARG GOARM=6

ENV GOARCH="$GOARCH" \
    GOARM="$GOARM"

RUN eval $(go env); \
	if [ "${GOARCH}" == "${GOHOSTARCH}" ]; then \
		echo "Building native arch"; \
		go build -v -o /go/bin/tpb-backup -a -installsuffix cgo \
		-ldflags "-linkmode external -extldflags \"-static\" -s -w"; \
	else \
		echo "Building for foreign arch $GOARCH on $GOHOSTARCH"; \
		go build -v -o /go/bin/tpb-backup -ldflags "-s -w"; \
	fi

RUN file /go/bin/tpb-backup | grep static

FROM scratch

ARG BUILD_DATE
ARG VCS_REF

LABEL org.label-schema.build-date=$BUILD_DATE \
      org.label-schema.vcs-url="https://github.com/brimstone/tpb-backup" \
      org.label-schema.vcs-ref=$VCS_REF \
      org.label-schema.schema-version="1.0.0-rc1"

COPY --from=builder /go/bin/tpb-backup /tpb-backup

ENTRYPOINT ["/tpb-backup"]
CMD []
