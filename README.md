# Chain-reactor for [Docker Hub]

Monitors base images and triggers builds of derivative ones:

```bash
docker run --rm -d -v dockerhub-chainreactor-data:/data \
-v /var/run/docker.sock:/var/run/docker.sock grandmaster/dockerhub-chainreactor
```

## Motivation

https://github.com/docker/hub-feedback/issues/1717

## Setup

Create `dockerhub-chainreactor-data/config.yml`:

```yaml
#log:
  # How verbosely to log (trace / debug / info / warn / error)
  #level: info
build:
  # When to build and deploy, crontab format
  every: '0 0 * * *'
hub:
  # URL to POST to ...
- post: https://hub.docker.com/api/build/v1/source/XXXX/trigger/YYYY/call/
  base:
  # ... on changes of these images:
  - debian:testing-slim
```

The daemon reloads its config automatically.

[Docker Hub]: https://hub.docker.com
