FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod ./
COPY cmd ./cmd
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/runtime ./cmd/runtime

FROM nousresearch/hermes-agent:latest
USER root
RUN mkdir -p /app/input /app/output /app/workspace && chown -R hermes:hermes /app
COPY --chown=hermes:hermes manifest.json /app/manifest.json
COPY --chown=hermes:hermes hermes_loop.py /app/hermes_loop.py
COPY --chown=hermes:hermes start.sh /app/start.sh
COPY --from=build /out/runtime /app/bin/runtime
RUN chmod 0755 /app/start.sh
USER hermes
ENV AGENT_RUNTIME_MANIFEST=/app/manifest.json HERMES_RUNTIME_GATEWAY_TIMEOUT_SECONDS=600
EXPOSE 8888
ENTRYPOINT ["/app/start.sh"]
