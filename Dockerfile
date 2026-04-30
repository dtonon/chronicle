FROM golang:bookworm AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o chronicle . \
    && go clean -modcache

FROM golang:bookworm
RUN mkdir -p /app/db
RUN mkdir -p /app/assets
COPY --from=build /app/chronicle /app
COPY --from=build /app/entrypoint.sh /app


WORKDIR /app
ENV RELAY_NAME="Chronicle Relay"
ENV RELAY_DESCRIPTION="Nostr Personal Relay"
ENV RELAY_PORT="3334"
ENV RELAY_URL=""
ENV RELAY_ICON=""
ENV RELAY_CONTACT=""
ENV DB_PATH="/app/db/"
ENV REFRESH_INTERVAL=24
ENV MIN_FOLLOWERS=3
ENV POW_WHITELIST=""
ENV POW_DM_WHITELIST=""
ENV BLOSSOM_PUBLIC_URL=""
ENV BLOSSOM_ASSETS_PATH="/app/assets/"
ENV BLOSSOM_BACKUP_MEDIA="TRUE"
ENV FETCH_ALL_INTERACTIONS="TRUE"
ENV SKIP_DELETIONS="FALSE"
VOLUME ["/app/db"]
VOLUME ["/app/assets"]
EXPOSE 3334
ENTRYPOINT ["/app/entrypoint.sh"]
