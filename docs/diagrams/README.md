# Ultraviolet — Diagrams

High-level mermaid diagrams. Render with the [Mermaid CLI](https://github.com/mermaid-js/mermaid-cli) (`mmdc -i X.mmd -o X.png`) or any Mermaid-aware viewer (GitHub, VS Code, Obsidian).

| File | What it shows |
|---|---|
| `01-high-level-architecture.mmd` | End-to-end system: clients → proxy → DuckDB / connectors → sync → storage → control plane |
| `02-query-routing.mmd` | Decision tree the router uses per query (DuckDB vs warehouse) |
| `03-cdc-sync-flow.mmd` | Sequence of warehouse → Iceberg → DuckDB refresh |
| `04-features.mmd` | Mind-map of product capabilities |
| `05-component-map.mmd` | Internal Go package layout under `cmd/` and `internal/` |

To render all to PNG:

```sh
for f in docs/diagrams/*.mmd; do
  mmdc -i "$f" -o "${f%.mmd}.png" -b transparent
done
```
