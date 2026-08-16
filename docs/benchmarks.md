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
| Phase 5 | Loki + Tempo + OTel + Alloy + existing monitoring | `<=900 MiB` | 844,398,592 bytes in run `29843478650` |
| Phase 5 | Standard-profile in-cluster total | `<=6.5 GiB` | 5,272,350,720 bytes in run `29843478650` |
| Phase 6 | Kyverno working set | `<=500 MiB` | 220,540,928 bytes |
| Phase 6 | Secured standard-profile in-cluster total | `<=7 GiB` | 6,059,945,984 bytes |
| Phase 7 | Data add-ons | `<=1.2 GiB` | 274.766 MiB in run `30910727236` |
| Phase 7 | Host SeaweedFS | `<=400 MiB` | 199.6 MiB in run `30910727236` |
| Phase 7 | Full-profile in-cluster total | `<=8 GiB` | 6151.422 MiB in run `30910727236` |
| Phase 7 | Whole-cluster RTO | `<=30m` | 12.58 minutes in run `30910727236` |
| Phase 7 | Confirmed archive RPO boundary | `<=5m` | 0 minutes in run `30910727236` |
| Phase 8 | Full-profile in-cluster total | `<=8 GiB` | 6172.121 MiB in run `31322270309` |
| Phase 8 | CLI status latency and binary size | informational | 25.31 ms and 37,915,296 bytes in run `31322270309` |
| Phase 9 | Portal process working set | `<=150 MiB` | 33,628,160 bytes in run `31943997118` |
| Phase 9 | Compressed JavaScript / CSS | `<=250 KiB` / `<=80 KiB` | 70,137 bytes / 7,730 bytes in deterministic control-ledger build |
| Phase 9 | In-cluster increase | `0` | embedded host-process architecture |

Phase 5 measures `container_memory_working_set_bytes` from Prometheus after telemetry and SLO checks have run. To distinguish the bounded steady working set from the intentional fast-burn load spike, both budgets must hold for three consecutive samples 15 seconds apart within a five-minute window. Zero/absent measurements and a budget that never stabilizes fail acceptance. Evidence records every sample, the final raw byte counts, timestamps, and a per-container observability breakdown; diagnostics capture the corresponding Pods and resource declarations.

Retention/storage caps are 24 hours and 4 GiB for Loki, and 24 hours and 2 GiB for Tempo. Both use disposable emptyDir storage. Prometheus retains six hours. These caps keep the standard profile bounded and deliberately avoid implying durable observability.

Phase 7 measures the data namespaces and bound workload through Prometheus,
the external SeaweedFS container through Docker, and the complete in-cluster
working set independently. Exact-main run `30910727236` also retained 33
external objects, including ten WAL objects, after finalizer-driven Database
deletion.

Phase 8 preserves the Phase 7 full-profile budget and adds no in-cluster CLI
component. Acceptance records the exact `platformctl` binary size and one
Application-status command latency beside the Prometheus working-set result.
These CLI measurements are diagnostic baselines, not hard performance promises;
the 8 GiB full-profile ceiling remains a blocking release gate.

Phase 9 retains that ceiling. Acceptance measures the embedded portal process
after real-cluster browser navigation and fails above 150 MiB. The tracked
asset manifest records sizes and SHA-256 values, and CI rejects build drift.
Branch acceptance run `31689255906` measured a 32,612,352-byte portal working
set and added no in-cluster component.
