# Arquitectura

Cada nodo es un proceso Go (`src/bully-algorithm`) que elige un coordinador, vigila su latido y registra la capacidad de su máquina en MongoDB. Los nodos se comunican por gRPC sobre Tailscale.

## Arquitectura hexagonal

El código se separa en tres capas. El dominio no importa gRPC, MongoDB ni APIs del sistema operativo.

| Capa | Ruta | Responsabilidad |
|---|---|---|
| Dominio | `internal/domain/model` | Reglas puras: prioridad Bully, reloj de Lamport, vector de versión y metadata del cluster. |
| Dominio | `internal/domain/usecase` | Casos de uso: elección, heartbeat, registro de nodos, vigilancia de followers y recursos. |
| Puertos | `internal/ports` | Interfaces que el dominio necesita (transporte, persistencia, membresía, capacidad de máquina). |
| Adaptadores | `internal/adapters/grpc` | Entrada (servidor gRPC) y salida (cliente gRPC hacia los demás nodos). |
| Adaptadores | `internal/adapters/persistence` | Repositorio MongoDB con transacciones y CAS. |
| Adaptadores | `internal/adapters/config` | Variables de entorno, archivo `.env` y lista de peers. |
| Adaptadores | `internal/adapters/system` | Lectura de CPU, memoria y velocidad de red del host. |

`cmd/main.go` es el único punto que conecta adaptadores con casos de uso.

## Componentes por nodo

1. **Elección Bully:** el nodo con mayor ID gana. Envía `RequestElection` a los IDs mayores y, si nadie responde dentro de `TIMEOUT_MS`, se proclama coordinador.
2. **Heartbeat:** los followers envían `ProcessHeartbeat` al coordinador cada `HEARTBEAT_MS`. Si no hay respuesta, inician una nueva elección.
3. **Vigilancia de followers:** el coordinador marca como `OFFLINE` a los followers que no envían latido durante más de `TIMEOUT_MS`.
4. **Registro de recursos:** cada follower envía `RegisterNode` con su capacidad al coordinador, y el coordinador escribe el documento en MongoDB.
5. **Persistencia:** el documento `cluster_metadata` y los documentos de nodo se actualizan dentro de una transacción.

## Flujo de un mensaje de elección

```text
Nodo candidato
  │ usecase: BullyElectionUseCase.Start
  │ port: ElectionTransportPort
  ▼
Adaptador gRPC (cliente)  ──RequestElection──►  Adaptador gRPC (servidor)
                                                  │ usecase: BullyElectionUseCase
                                                  ▼
                                               Respuesta acknowledged=true
```

El dominio decide quién responde y cuándo. El adaptador solo traduce el mensaje a gRPC.

## Límite de la arquitectura

La separación en capas no resuelve el split-brain. Si dos nodos se creen coordinadores durante una partición, el sistema no tiene término de elección ni fencing token. La decisión y sus consecuencias están en `docs/consistencia.md`.
