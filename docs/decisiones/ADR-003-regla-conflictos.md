# ADR-003: Detección y resolución de conflictos

## Contexto

Durante una partición de red, dos nodos pueden proclamarse coordinadores
simultáneamente y escribir sobre el mismo recurso del inventario. Al reconectar
la red, el sistema debe detectar esas escrituras concurrentes y aplicar una
regla determinista que deje el inventario en un estado único y coherente.

## Detección


Dos escrituras sobre el mismo recurso son **concurrentes** si ninguno de sus
vectores de versión domina al otro, es decir:

```
A.vector.Compare(B.vector) == VersionConcurrent
```

La función `VersionVector.Compare` implementada en
`internal/domain/model/version_vector.go` devuelve:

| Resultado           | Significado                               |
| ------------------- | ----------------------------------------- |
| `VersionEqual`      | Ambas escrituras son idénticas en versión |
| `VersionBefore`     | A es más antigua que B                    |
| `VersionAfter`      | A es más reciente que B                   |
| `VersionConcurrent` | **Conflicto**: ninguna domina a la otra   |

Un reloj de Lamport por sí solo **no detecta concurrencia**: solo ordena
eventos, no establece si fueron simultáneos. Por eso el sistema usa el
vector de versión para la detección y el Lamport solo para el desempate.

Cada documento de recurso incluye en su campo `metadata`:

```json
{
  "lamport": 7,
  "version_vector": { "1": 3, "2": 2, "3": 0 },
  "updated_by": 1
}
```

Al reconectar un nodo aislado, el sistema lee la versión local y la versión
remota del mismo recurso y llama a `Resource.ConflictsWith(other)`, que
internamente usa `Metadata.CompareVersion`.

## Regla de resolución

Cuando se detecta `VersionConcurrent`, se aplica la siguiente regla
implementada en `model.ResolveConflict` (`internal/domain/model/resource.go`):

1. **Gana la escritura con el reloj de Lamport mayor.** El Lamport captura el
   orden causal global: un valor más alto implica que ese evento ocurrió
   causalmente después de más eventos en el sistema.
2. **En caso de empate de Lamport, gana el nodo con el ID menor.** Esta regla
   es arbitraria pero determinista: todos los nodos que evalúen el mismo par de
   versiones llegarán al mismo ganador sin coordinación adicional.

Tras elegir el ganador, se fusionan los vectores con `Merge` (máximo por
componente) y se conserva el Lamport mayor. Esto garantiza que la versión
resultante sea vista como descendiente de ambas versiones en conflicto:

```go
winner.Metadata.Lamport = max(left.Lamport, right.Lamport)
winner.Metadata.VersionVector = left.VersionVector.Merge(right.VersionVector)
```

### Ejemplo

|                             | Nodo 1 (aislado)             | Nodo 2 (nuevo coordinador)   |
| --------------------------- | ---------------------------- | ---------------------------- |
| Vector antes de partición   | `{1:2, 2:1, 3:0}`            | `{1:2, 2:1, 3:0}`            |
| Escritura durante partición | `{1:3, 2:1, 3:0}`, Lamport=6 | `{1:2, 2:2, 3:0}`, Lamport=5 |
| Compare                     | —                            | `VersionConcurrent`          |
| Ganador                     | **Nodo 1** (Lamport 6 > 5)   | —                            |
| Vector resultante           | `{1:3, 2:2, 3:0}`, Lamport=6 | —                            |

## Registro


El sistema registra en el log de cada nodo:

- Cada conflicto detectado: `conflict detected resource=<id> local_lamport=<n> remote_lamport=<n>`.
- El ganador de cada resolución: `conflict resolved resource=<id> winner_node=<id> lamport=<n>`.
- Contadores acumulados al final del proceso de reconciliación:
  `reconciliation complete conflicts_detected=<n> conflicts_resolved=<n>`.

## Consecuencias

- La regla es **determinista y sin coordinación**: cualquier nodo que observe
  el mismo par de versiones elige el mismo ganador.
- La regla puede descartar una escritura válida del nodo con el reloj más bajo.
  Esto es aceptable para un inventario de capacidades físicas, donde la versión
  más reciente es la más relevante.
- El sistema no implementa reconciliación de tres vías (_three-way merge_). Si
  se requiriera en el futuro, habría que registrar la versión base común.
