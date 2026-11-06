# Hermes runtime

This sandbox ports the useful Hermes core into `agent-platform-runtime/v1`: a
bounded tool-calling event loop, MCP discovery/dispatch, and lazy Skill loading.
It calls the stage-scoped model endpoint in `run.json` directly and exposes only
the runtime HTTP transport on port 8888.

It deliberately excludes message platforms, schedulers, desktop/web/IDE
integration, plugins, browser tools, tests, evaluations and documentation
resources. Attached Skills are unpacked under `/app/workspace/.skills` and must
be loaded explicitly with `agent_load_skill`; MCP tools are limited to the
server operations selected in `run.json`.

```sh
docker build -f runtimes/sandbox-runtime-hermes/Dockerfile runtimes/sandbox-runtime-hermes
```
