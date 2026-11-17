FROM golang:1.25.5-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -o /out/forum .

FROM debian:bookworm-slim
RUN groupadd --gid 10001 forum && useradd --uid 10001 --gid forum --no-create-home forum && mkdir -p /app/data && chown forum:forum /app/data && chmod 700 /app/data
WORKDIR /app
COPY --from=build /out/forum /app/forum
COPY web /app/web
USER 10001:10001
EXPOSE 8080
VOLUME ["/app/data"]
ENTRYPOINT ["/app/forum"]
CMD ["-addr", "0.0.0.0:8080", "-db", "/app/data/forum.db"]
