# ADR-0003: Use Envoy Gateway and Gateway API

## Context

SteadyState requires maintained north-south routing now and weighted traffic routing later. The originally considered community ingress-nginx controller was retired in 2026.

## Decision

Use the Kubernetes Gateway API with Envoy Gateway. Local access uses deterministic NodePorts mapped by kind to configurable loopback ports.

## Consequences

Routing uses portable Kubernetes APIs and has a maintained path to weighted canary traffic. Installation has more CRDs than basic Ingress and requires explicit service customization for kind.
