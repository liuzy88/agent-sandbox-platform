FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/runtime ./cmd/runtime

FROM nousresearch/hermes-agent:latest
USER root
RUN mkdir -p /app/input /app/output /app/workspace && chown -R hermes:hermes /app
COPY --chown=hermes:hermes manifest.json /app/manifest.json
COPY --from=build /out/runtime /app/bin/runtime
COPY --chown=hermes:hermes start.sh /app/start.sh
RUN chmod 0755 /app/start.sh
USER hermes
ENV AGENT_RUNTIME_MANIFEST=/app/manifest.json HERMES_RUNTIME_GATEWAY_URL=http://127.0.0.1:8642 HERMES_RUNTIME_GATEWAY_TIMEOUT_SECONDS=600 API_SERVER_ENABLED=true
EXPOSE 8888
ENTRYPOINT ["/app/start.sh"]
