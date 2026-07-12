# Phase 0 Architecture

```mermaid
flowchart TB
    subgraph Host["Windows host"]
        Git["Git for Windows"]
        Dev["scripts/dev.ps1"]
        Tools["Repository-local tools"]
        Docker["Docker Desktop"]
    end
    subgraph Cluster["kind: steadystate"]
        CP["Control plane"]
        Workers["0-2 workers"]
        Calico["Calico CNI"]
        EG["Envoy Gateway"]
        Gateway["GatewayClass / Gateway"]
        Route["HTTPRoute"]
        Echo["Smoke workload"]
    end
    Dev --> Tools
    Dev --> Docker
    Docker --> Cluster
    CP --> Calico
    Workers --> Calico
    EG --> Gateway --> Route --> Echo
    Dev -->|"127.0.0.1:8080 to NodePort 30080"| EG
    Git -. "never uses WSL" .-> Dev
```

## Profiles

| Profile | Nodes | Intended use |
|---|---:|---|
| `minimal` | 1 control plane | Pull-request smoke tests and constrained machines |
| `standard` | 1 control plane + 1 worker | Default development profile |
| `full` | 1 control plane + 2 workers | Later end-to-end demonstrations |

Every profile disables kindnet and installs Calico, making NetworkPolicy behavior observable. Envoy Gateway provides the maintained Gateway API implementation for north-south traffic.

Phase 0 owns cluster creation, networking, Gateway API installation, smoke resources, and diagnostics. Operator APIs, GitOps reconciliation, progressive delivery, policy admission, observability, and stateful recovery enter in later verified phases.
