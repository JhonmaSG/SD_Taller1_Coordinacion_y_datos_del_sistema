# ADR-001: Algoritmo de elección

Opciones consideradas: Bully (Garcia-Molina, 1982) / Anillo (Chang y Roberts, 1979).

## Decisión

Se implementó el **algoritmo del Matón (Bully)** de Garcia-Molina (1982).

Cada nodo tiene un identificador numérico entero positivo (`NODE_ID`). La
prioridad Bully es directamente proporcional al identificador: el nodo con el
ID mayor gana cualquier elección.

El protocolo usa tres tipos de mensajes transportados sobre gRPC:

| Mensaje gRPC | Equivalente Bully | Descripción |
|---|---|---|
| `RequestElection` | ELECTION | El candidato pregunta a los nodos con ID mayor si están vivos |
| `RequestElectionResponse` (acknowledged=true) | OK | Un nodo superior confirma que tomará el liderazgo |
| `AnnounceCoordinator` | COORDINATOR | El ganador notifica a todos que es el nuevo coordinador |

Flujo de una elección iniciada por el nodo _i_:

1. El nodo _i_ envía `RequestElection` en paralelo a todos los peers con `ID > i`.
2. Si ninguno responde dentro de `TIMEOUT_MS`, _i_ se proclama coordinador y
   envía `AnnounceCoordinator` a todos los peers.
3. Si algún peer con ID mayor responde `acknowledged=true`, _i_ queda en estado
   `WaitingForCoordinator` y espera recibir `AnnounceCoordinator` de ese peer.
4. Al recibir `AnnounceCoordinator`, cualquier nodo actualiza su coordinador
   conocido. Si el mensaje llega al nodo local como coordinador, confirma su
   propio liderazgo.

El coordinador envía un latido periódico (`ProcessHeartbeat`) al que los
followers responden con `alive=true`. Si un follower no recibe respuesta dentro
de `TIMEOUT_MS`, inicia una nueva elección.

## Justificación

Se eligió Bully sobre el algoritmo de Anillo por las siguientes razones:

- **Convergencia rápida en grupos pequeños**: con solo 3 nodos, el Bully
  termina en una ronda si el nodo con mayor ID está disponible. El anillo
  requeriría que el mensaje circule por todos los nodos incluso cuando el
  ganador es evidente.
- **Sin topología adicional**: el Bully no exige mantener un orden lógico de
  anillo. Basta con la lista plana de peers (`PEERS`), que ya se usa para los
  heartbeats y el registro de nodos.
- **Determinismo**: el ganador siempre es el nodo con el ID más alto entre los
  disponibles. No hay aleatoriedad ni votos. Esto simplifica la depuración y
  la reproducción de escenarios.
- **Implementación directa sobre gRPC**: los tres mensajes del Bully se mapean
  directamente a tres RPCs del servicio `ElectionService`, sin necesidad de
  gestionar un estado de anillo compartido.

## Consecuencias

- Un nodo con ID alto que se reinicia puede expulsar al coordinador actual
  iniciando una nueva elección. Esto es el comportamiento esperado del Bully.
- Durante una partición de red, dos nodos pueden proclamarse coordinadores
  simultáneamente (*split-brain*). El sistema no implementa fencing tokens ni
  épocas de elección; la barrera de escritura en MongoDB (`writeConcern=majority`)
  mitiga el riesgo de escrituras conflictivas durante la partición.
- La elección es O(n²) en mensajes en el caso peor. Para 3 nodos esto es
  irrelevante.
