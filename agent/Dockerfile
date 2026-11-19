# syntax=docker/dockerfile:1.7
FROM alpine:3.20 AS build
RUN apk add --no-cache build-base
WORKDIR /src
COPY . .
# Tests run inside the image build too, against musl: the libc real terminals ship with.
RUN make test && make static

FROM alpine:3.20
RUN adduser -S -D agent
COPY --from=build /src/build/satfleet-agent /usr/local/bin/satfleet-agent
USER agent
ENTRYPOINT ["satfleet-agent"]
