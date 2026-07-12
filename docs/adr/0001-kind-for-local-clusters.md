# ADR-0001: Use kind for Local Kubernetes Clusters

## Context

SteadyState needs disposable multi-node Kubernetes clusters that run on a laptop and in GitHub-hosted Linux runners. Cluster configuration and Kubernetes versions must be pinned.

## Decision

Use kind with digest-pinned node images and explicit minimal, standard, and full profiles.

## Consequences

Clusters are reproducible and inexpensive to recreate. Docker is required, and persistent volumes disappear with the cluster unless external storage is deliberately used.
