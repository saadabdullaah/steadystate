# Resource Budgets and Hosted Measurements

SteadyState's limits are acceptance boundaries for a disposable laptop-scale platform, not production sizing guidance.

| Milestone | Measurement | Budget | Last verified result |
|---|---|---:|---|
| Phase 0 | Windows standard bootstrap | informational | 8.9 minutes |
| Phase 1 | Deployment recreation | `<10s` | 0.300 seconds |
| Phase 1 | Replica drift repair | `<10s` | 0.435 seconds |
| Phase 4 | Monitoring working set | `<=1.2 GiB` | 383,983,616 bytes |
| Phase 4 | Good canary completion | `<=12m` | passed in run `29681093123` |
| Phase 4 | Bad-candidate abort | `<=180s` after 10% | passed in run `29681093123` |
| Phase 5 | Loki + Tempo + OTel + Alloy + existing monitoring | `<=900 MiB` | pending Phase 5 hosted artifact |
| Phase 5 | Standard-profile in-cluster total | `<=6.5 GiB` | pending Phase 5 hosted artifact |

Phase 5 measures `container_memory_working_set_bytes` from Prometheus after telemetry and SLO checks have run. Zero/absent measurements fail acceptance rather than passing as an empty result. The evidence JSON records raw byte counts and timestamps; diagnostics capture the corresponding Pods and resource declarations.

Retention/storage caps are 24 hours and 4 GiB for Loki, and 24 hours and 2 GiB for Tempo. Both use disposable emptyDir storage. Prometheus retains six hours. These caps keep the standard profile bounded and deliberately avoid implying durable observability.
