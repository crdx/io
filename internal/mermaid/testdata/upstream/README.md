# Upstream Mermaid fixtures

These tests and rendering fixtures were imported from [`AlexanderGrooff/mermaid-ascii`](https://github.com/AlexanderGrooff/mermaid-ascii) version 1.5.0 at commit `b1b35f67d6a5dd0699ccfc968c00a763db573076`.

The upstream MIT licence is retained at `internal/mermaid/LICENSE`. Test files prefixed with `upstream_` preserve the applicable parser, renderer, layout, and regression cases without adding upstream's test-only dependencies.

Five ASCII subgraph fixtures intentionally differ because this fork includes external-node offsets in subgraph borders. `upstream_golden_test.go` names those fixtures explicitly and requires every other rendering to match upstream.
