FROM golang:bookworm AS build
WORKDIR /app
COPY . .
RUN go mod download \
    && go build -tags=lmdb -ldflags "-X main.version=$(git describe --tags --always)" -o chronicle . \
    && go clean -modcache

FROM golang:bookworm
COPY --from=build /app /app


WORKDIR /app
ENV RELAY_NAME="Cronicle Relay"
ENV RELAY_DESCRIPTION="Nostr Personal Relay"
ENV RELAY_PORT="3334"
ENV DB_PATH="db/"
ENV REFRESH_INTERVAL=24
ENV MIN_FOLLOWERS=3
ENV FETCH_SYNC="FALSE"
VOLUME ["/app/db"]
EXPOSE 3334
ENTRYPOINT ["/app/entrypoint.sh"]
