# tinysystems-otel-collector

A minimalistic Helm chart for deploying OpenTelemetry Collector.

## Installation

```bash
helm install my-otel-collector ./tinysystems-otel-collector
```

## Configuration

The following table lists the key configurable parameters:

| Parameter | Description | Default |
|-----------|-------------|---------|
| `replicaCount` | Number of replicas | `1` |
| `image.repository` | Image repository | `ghcr.io/tiny-systems/otel-collector` |
| `image.tag` | Image tag | `main` |
| `service.type` | Service type | `ClusterIP` |
| `service.port` | Service port | `4317` |
| `resources.limits.cpu` | CPU limit | `500m` |
| `resources.limits.memory` | Memory limit | `512Mi` |

## Example Custom Values

```yaml
replicaCount: 2

resources:
  limits:
    cpu: 1000m
    memory: 1Gi
  requests:
    cpu: 200m
    memory: 256Mi
```

Install with custom values:

```bash
helm install my-otel-collector ./tinysystems-otel-collector -f custom-values.yaml
```
