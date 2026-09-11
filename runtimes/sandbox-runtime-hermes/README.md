# Hermes runtime

`sandbox-runtime-hermes` is a standalone Go runtime project which adapts the
official Hermes Agent gateway to the project's `agent-platform-runtime/v1`
contract. The image starts Hermes' local OpenAI-compatible API at port 8642
and exposes the platform transport at port 8888.

```bash
docker build -f sandbox-runtime-hermes/Dockerfile -t agent-platform/sandbox-runtime:hermes sandbox-runtime-hermes
```

Mount a configured `/opt/data` volume or provide Hermes' normal provider
configuration. Set a non-default `API_SERVER_KEY`; it authenticates the
adapter to the loopback Hermes API. The statically-linked Go adapter reads
`/app/run.json`, emits
the final assistant content as an `agent.token` event, and atomically writes
`/app/output/result.json`. Register `manifest.json` explicitly: its
`hermes-runtime/1` checkpoints are not compatible with the other runtimes.
