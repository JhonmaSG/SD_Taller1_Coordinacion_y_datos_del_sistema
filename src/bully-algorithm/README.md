# Nodo distribuido de inventario

Este módulo contiene el proceso Go que ejecuta cada nodo del sistema. Implementa:

- Elección de coordinador con el algoritmo del Matón (Bully) sobre gRPC.
- Detección de caída del coordinador mediante heartbeats.
- Registro de la capacidad física de cada nodo como documento en MongoDB.
- Escritura exclusiva del coordinador sobre la colección de nodos.
- Vector de versiones y reloj de Lamport globales por cluster, con comparación causal.

Todavía no implementa: asignación de recursos por RPC, detección de particiones
de red (split-brain), términos de elección ni fencing. Esas partes están listadas
en la sección 13.

---

## 1. Ejecución

Todos los comandos se ejecutan desde `src/bully-algorithm`.

```powershell
Set-Location src/bully-algorithm
Copy-Item ..\..\.env.example .env   # primera vez; luego editar .env
go run ./cmd
```

El proceso carga `.env` si existe. Las variables ya definidas en el entorno
tienen prioridad sobre el archivo.

### Tres nodos en la misma máquina

Opciones:

- **Script** (Git Bash o WSL, desde la raíz del repositorio):

  ```bash
  bash scripts/levantar_nodos_local.sh
  ```

  Compila el binario una vez y levanta los nodos 1, 2 y 3 en `127.0.0.1:5001`,
  `5002` y `5003`. Los logs quedan en `.runtime/local-nodes/logs/`.

- **Manual**: una terminal por nodo, con `$env:` para `NODE_ID`, `NODE_PORT` y
  `PEERS`. Cada `$env:` tiene prioridad sobre el `.env`.

### Despliegue en VMs

```bash
NODES="usuario1@100.x.x.1 usuario2@100.x.x.2 usuario3@100.x.x.3" \
  bash src/bully-algorithm/scripts/deploy_nodos.sh
```

`scripts/deploy_nodos.sh` compila un binario `linux/amd64` estático y lo copia
por SSH a cada VM (`/home/<usuario>/test/nodo` por defecto). No modifica los
`.env` de los nodos; cada VM debe tener su propio `.env`.

---

## 2. Configuración

Variables leídas por `internal/adapters/config/env.go`:

| Variable | Obligatoria | Por defecto | Descripción |
|---|---|---|---|
| `NODE_ID` | Sí | — | Identificador numérico del nodo (entero positivo). Define la prioridad Bully. |
| `NODE_HOST` | Sí | — | IP o host con el que el nodo se anuncia a los demás. |
| `NODE_PORT` | No | `5000` | Puerto del servidor gRPC. |
| `LISTEN_ADDRESS` | No | `NODE_HOST:NODE_PORT` | Dirección de escucha, si es distinta del anuncio. |
| `PEERS` | No | vacío | Lista `<id>@<host>:<port>` separada por comas. Sin el ID no hay prioridad. |
| `TIMEOUT_MS` | No | `3000` | Límite de cada RPC de elección, de cada heartbeat y del anuncio `COORDINATOR`. También es el tiempo sin heartbeats a partir del cual el coordinador marca un follower como `OFFLINE`. |
| `HEARTBEAT_MS` | No | `1000` | Periodo del monitor de heartbeats y de la sincronización de registro. |
| `DB_URI` | Sí | — | URI de MongoDB. Para el Replica Set: `?replicaSet=rs-grupo`. |
| `DB_NAME` | No | `inventory` | Base de datos. |
| `DB_NODE_COLLECTION` | No | `nodes` | Colección con un documento por nodo. |

Notas:

- `DB_COLLECTION` (nombre antiguo) ya no se usa en el flujo de nodos.
- CPU, memoria y ancho de banda **no** se configuran. Se leen de la máquina
  (sección 9.4).
- `PEERS` no debe incluir al nodo local. La validación lo rechaza.

Ejemplo para el nodo 1:

```env
NODE_ID=1
NODE_HOST=100.x.x.1
NODE_PORT=5000
PEERS=2@100.x.x.2:5000,3@100.x.x.3:5000
TIMEOUT_MS=3000
HEARTBEAT_MS=1000
DB_URI=mongodb://100.x.x.1:27017,100.x.x.2:27017,100.x.x.3:27017/?replicaSet=rs-grupo&w=majority&wtimeoutMS=5000&retryWrites=true
DB_NAME=inventory
DB_NODE_COLLECTION=nodes
```

---

## 3. Arquitectura hexagonal

El código separa el dominio de la infraestructura. El dominio (`internal/domain`)
no importa gRPC, MongoDB, BSON ni APIs del sistema operativo.

```text
src/bully-algorithm/
├── cmd/
│   └── main.go                      # Composición de dependencias y ciclo de vida
├── proto/election/v1/
│   └── election.proto               # Contrato gRPC
├── election/v1/
│   ├── election.pb.go               # Generado por buf
│   └── election_grpc.pb.go          # Generado por buf
├── internal/
│   ├── domain/
│   │   ├── model/                   # Entidades y reglas puras
│   │   │   ├── node.go              # Node, NodeID, prioridad Bully, Key()
│   │   │   ├── election.go          # ElectionState, ElectionResult
│   │   │   ├── lamport.go           # LamportClock
│   │   │   ├── version_vector.go    # VersionVector: Compare, Merge, Increment
│   │   │   ├── node_document.go     # NodeDocument, roles y recursos
│   │   │   └── cluster_metadata.go  # Metadata global del cluster
│   │   └── usecase/
│   │       ├── bully_election_usecase.go       # Elección Bully
│   │       ├── cluster_metadata_usecase.go     # Metadata global y liderazgo
│   │       ├── follower_liveness_usecase.go    # OFFLINE de followers
│   │       ├── process_heartbeat_usecase.go    # Monitor de coordinador
│   │       ├── register_node_usecase.go        # Registro de nodos y roles
│   │       └── update_node_resources_usecase.go# Actualización de recursos
│   ├── ports/                        # Interfaces que el dominio necesita
│   │   ├── membership.go             # MembershipPort
│   │   ├── election_transport.go     # ElectionTransportPort (ELECTION, COORDINATOR, heartbeat)
│   │   ├── node_registration.go      # NodeRegistrationTransport (RegisterNode)
│   │   ├── node_document_repository.go # NodeDocumentRepository + errores de dominio
│   │   ├── coordinator_view.go       # CoordinatorView (autoridad local)
│   │   ├── machine_capacity.go       # MachineCapacityReader
│   │   └── follower_status.go         # FollowerStatusWriter
│   └── adapters/
│       ├── config/env.go             # Variables de entorno, .env, PEERS
│       ├── grpc/
│       │   ├── election_server.go    # Servidor gRPC (entrada)
│       │   └── election_transport.go # Cliente gRPC (salida)
│       ├── persistence/
│       │   └── mongo_repository.go   # MongoNodeDocumentRepository (nodes)
│       ├── system/
│       │   └── machine_capacity.go   # Lectura de CPU, RAM y red del host
├── scripts/deploy_nodos.sh
├── buf.yaml / buf.gen.yaml
├── go.mod / go.sum
└── README.md
```

### Dominio

- **`Node` y `NodeID`**: identidad numérica. La prioridad Bully es `ID` mayor.
  `NodeID.Key()` devuelve la clave textual `node_<id>`, usada en vectores y en
  MongoDB.
- **`LamportClock`**: `Tick()` para eventos locales y `Observe(remote)` para
  recepciones. La regla es `max(local, remoto) + 1`.
- **`VersionVector`**: `map[string]uint64` con claves `node_<id>`. Permite
  `Increment`, `Merge` (máximo por nodo) y `Compare`, que devuelve
  `VersionEqual`, `VersionBefore`, `VersionAfter` o `VersionConcurrent`.
- **`ClusterMetadata`**: reloj global, vector global, coordinador activo y nodo
  que produjo la última escritura.
- **`NodeDocument`**: agregado de recursos y rol de un nodo. No contiene
  metadata de concurrencia; esa metadata vive en `cluster_metadata`.

### Puertos

Los puertos son interfaces definidas en `internal/ports`. Los adaptadores los
implementan:

| Puerto | Implementado por |
|---|---|
| `MembershipPort` | `config.StaticMembership` |
| `ElectionTransportPort` | `grpc.ElectionTransport` |
| `NodeRegistrationTransport` | `grpc.ElectionTransport` |
| `NodeDocumentRepository` | `persistence.MongoNodeDocumentRepository` |
| `CoordinatorView` | `usecase.BullyElectionUseCase` |
| `MachineCapacityReader` | `system.MachineCapacityReader` |
| `FollowerStatusWriter` | `RegisterNodeUseCase` |

`NodeDocumentRepository` expone `Get`, `GetClusterMetadata`,
`UpdateClusterMetadataAtomic` y `UpdateAtomic`. Las actualizaciones de un nodo
reciben metadata global como precondición y modifican los dos documentos dentro
de una transacción; `ErrVersionConflict` indica que el cluster cambió desde la
lectura.

---

## 4. Arranque del proceso (`cmd/main.go`)

El orden actual es:

```text
1. Cargar .env (opcional) y validar la configuración
2. Crear membresía estática (local + peers)
3. Crear BullyElectionUseCase (timeout TIMEOUT_MS)
4. Crear ProcessHeartbeatUseCase (HEARTBEAT_MS)
5. Crear ElectionServer y abrir el listener TCP
6. Iniciar el servidor gRPC en una goroutine
7. Ejecutar la elección inicial (Start, bloqueante hasta TIMEOUT_MS)
8. Iniciar el monitor de heartbeats
9. Conectar a MongoDB (initializeRepository)
     - Si falla: se registra advertencia y el nodo sigue en modo degradado
10. Si hay conexión:
      - Crear RegisterNodeUseCase
      - Registrar el nodo (Register, con timeout de 10 s)
      - Conectar el caso de uso al servidor gRPC (SetRegistrationUseCase)
      - Iniciar el bucle de registro periódico (Run)
      - Crear FollowerLivenessUseCase (umbral TIMEOUT_MS) y su bucle de barrido
      - Al apagar: MarkOffline (solo si el nodo es coordinador)
11. Esperar SIGINT/SIGTERM o un error del servidor
12. GracefulStop del servidor gRPC y cierre de MongoDB
```

Si falla la configuración o la escucha TCP, el proceso termina con error. Si
MongoDB no está disponible, el nodo sigue participando en Bully y reintenta la
conexión cada `5s`. Cuando MongoDB vuelve a responder, el proceso conecta el
repositorio, registra el nodo y activa la vigilancia de followers sin reiniciar.

---

## 5. Algoritmo Bully

### 5.1 Elección

`BullyElectionUseCase.Start`:

1. Obtiene la membresía y excluye al nodo local de la lista de peers.
2. Selecciona los peers con ID mayor que el local.
3. Envía `RequestElection` a esos peers en paralelo.
4. Si alguno responde `acknowledged=true`, el nodo queda en
   `ElectionWaitingForCoordinator` y espera un `AnnounceCoordinator`.
5. Si ninguno responde dentro de `TIMEOUT_MS`, el nodo se proclama
   `ElectionCoordinator` y envía `AnnounceCoordinator` a todos los peers, con un
   timeout propio.

Un vencimiento de contexto se interpreta como "peer no disponible", no como
fallo de la elección. Solo una cancelación explícita aborta la ronda.

### 5.2 Mensajes entrantes

- `RequestElection`: si el receptor tiene ID mayor que el candidato, responde
  `acknowledged=true` e inicia su propia elección en segundo plano.
- `AnnounceCoordinator`: valida que el coordinador pertenezca a la membresía,
  actualiza el estado local a `ElectionFollowing` o `ElectionCoordinator` y
  responde `accepted=true`. Si el nodo es follower, también ejecuta el registro
  de su documento (sección 6).

### 5.3 Heartbeat

`ProcessHeartbeatUseCase` corre cada `HEARTBEAT_MS`. Si el nodo es follower,
envía `ProcessHeartbeat` al coordinador conocido:

- Respuesta `alive=true`: no hace nada.
- Error o timeout: registra el fallo e inicia una nueva elección.

El coordinador responde `alive=true` solo si el mensaje llega dirigido a él y
si, en su propio estado, sigue siendo coordinador.

El coordinador también usa estos mismos heartbeats para vigilar a los followers
(diseño pasivo): cada vez que responde `alive=true` anota la hora del último
heartbeat del emisor en `FollowerLivenessUseCase`. No hay tráfico adicional.

### 5.4 Vigilancia de followers por el coordinador

Lo ejecuta `FollowerLivenessUseCase`, en el arranque (sección 4, paso 10):

- Al ser elegido, el coordinador asigna a cada peer un periodo de gracia de
  `TIMEOUT_MS`, contado desde el primer barrido.
- Cada `HEARTBEAT_MS` ejecuta un barrido. Si un follower no envió heartbeat
  durante más de `TIMEOUT_MS`, el coordinador lo marca `OFFLINE` con rol
  `FOLLOWER` mediante una escritura CAS.
- Un follower marcado se marca una sola vez hasta que vuelva a enviar
  heartbeats. Su documento vuelve a `ONLINE` cuando se registra de nuevo con el
  coordinador (`RegisterNode`).
- Si el nodo deja de ser coordinador, olvida el estado del liderazgo anterior.

---

## 6. Documentos de nodos y metadata global

La colección `DB_NODE_COLLECTION` contiene un documento de control del cluster y
un documento de recursos por cada nodo.

### 6.1 Documento `cluster_metadata`

Este documento concentra el reloj y el vector global:

```json
{
  "_id": "cluster_metadata",
  "global_lamport_clock": 14,
  "version_vector": {
    "node_1": 5,
    "node_2": 3,
    "node_3": 6
  },
  "active_coordinator": "node_3",
  "last_modified_by": "node_3"
}
```

### 6.2 Documentos de recursos por nodo

Cada nodo tiene un documento separado. Estos documentos no contienen Lamport ni
vector de versión:

```json
{
  "_id": "node_3",
  "node_role": "COORDINATOR",
  "resources": {
    "cpu_cores": 12,
    "memory_gb": 31,
    "bandwidth_mbps": 100000
  },
  "status": "ONLINE"
}
```

Los campos son:

- `node_role`: `COORDINATOR` o `FOLLOWER`.
- `status`: `ONLINE` u `OFFLINE`.
- `resources`: capacidad física detectada en el host.
- `bandwidth_mbps` se conserva para cada nodo.

`MongoNodeDocumentRepository` lee el documento de nodo y le adjunta en memoria
la metadata global. Al persistir, reemplaza el documento de nodo y el documento
`cluster_metadata` dentro de una transacción MongoDB.

### 6.3 Registro de followers


```text
Follower
  │ lee CPU, RAM y red del host (una vez por proceso)
  │ RegisterNode(sender, resources, metadata) ──────────────► Coordinador
  │                                                              │
  │                                              RegisterRemote: │
  │                                              - valida el sender
  │                                              - fusiona vectores
  │                                              - incrementa su contador
  │                                              - UpdateAtomic (CAS)
  │◄──────────────────────────────── RegisterNodeResponse ───────┘
```

`RegisterNode` es la única vía por la que un follower logra que se escriba su
documento. Si el coordinador no es conocido todavía, el follower no hace nada y
reintenta en el siguiente ciclo.

### 6.4 Registro del coordinador

El coordinador escribe su propio documento de recursos y actualiza
`cluster_metadata.active_coordinator`, `global_lamport_clock`,
`version_vector` y `last_modified_by`. Si no cambió el rol, la capacidad ni el
vector requerido, no genera una escritura innecesaria.

### 6.5 Cambio de coordinador

Cuando un nodo recibe `AnnounceCoordinator` y no es el coordinador:

1. Actualiza su estado a follower.
2. Ejecuta `Register`, que envía `RegisterNode` al nuevo coordinador.
3. El nuevo coordinador reescribe el documento del follower con rol
   `FOLLOWER` y `last_modified_by` igual a su propio ID.

Un follower ya registrado con el mismo rol, recursos y coordinador actual no
produce escritura: el coordinador devuelve el documento sin cambios.

### 6.6 Vector global y Lamport

El vector y el reloj ya no se almacenan en cada documento `node_<id>`. Viven en
`cluster_metadata` y se incrementan una vez cuando cambia el coordinador activo.
La escritura que registra el nuevo liderazgo actualiza `active_coordinator`,
`global_lamport_clock`, la posición del nuevo coordinador y
`last_modified_by`. Las escrituras posteriores de documentos de recursos usan
la metadata global vigente sin incrementar el vector otra vez por cada documento.

La metadata global se compara por CAS. Cada contador se filtra por separado en
MongoDB para que el orden de las claves del mapa no produzca falsos conflictos.

### 6.7 Marcado `OFFLINE` de followers caídos

Lo ejecuta solo el coordinador, mediante `RegisterNodeUseCase.MarkFollowerOffline`:

1. Lee el documento del follower.
2. Si ya está `OFFLINE` y es `FOLLOWER`, no hace nada.
3. Si no, escribe `status=OFFLINE`, `node_role=FOLLOWER`, avanza el Lamport
   del coordinador, incrementa su contador en el vector y actualiza
   `last_modified_by`.
4. La escritura usa CAS. Si hay conflicto, el barrido siguiente lo reintenta.

El coordinador no se marca a sí mismo. El nodo que deja de ser coordinador
conserva su documento hasta que el nuevo coordinador lo reescriba como
follower o lo marque `OFFLINE`.

---

## 7. Autoridad de escritura

Solo el coordinador local puede escribir documentos. La regla se aplica en
varios niveles:

1. **Casos de uso**: `RegisterRemote`, `registerLocal`,
   `UpdateNodeResourcesUseCase.Update` y `MarkOffline` verifican
   `CoordinatorView.IsLocalCoordinator()`.
   Esto incluye `MarkFollowerOffline` y `FollowerLivenessUseCase.Sweep`.
2. **Repositorio**: `MongoNodeDocumentRepository.UpdateAtomic` vuelve a
   verificarlo antes de ejecutar la transacción que actualiza `cluster_metadata`
   y el documento del nodo. Si no es coordinador, devuelve `ErrNotCoordinator`.
3. **Repositorio legado**: `ResourceRepository.Save` aplica la misma barrera.

`IsLocalCoordinator()` consulta el estado de Bully en memoria. No depende de
lo que diga el campo `node_role` de MongoDB.

Esta barrera protege contra escrituras de un nodo que cree no ser coordinador.
No protege contra dos nodos que se crean coordinadores al mismo tiempo durante
una partición. Ese caso se describe en la sección 13.

---

## 8. Persistencia MongoDB

- La conexión usa `mongo.Connect` con la URI `DB_URI` y verifica con `Ping`.
- La colección de nodos es `DB_NODE_COLLECTION` (`nodes` por defecto).
- Las escrituras usan `ReplaceOne` dentro de una transacción MongoDB. El CAS
  filtra `global_lamport_clock`, `last_modified_by` y cada contador del vector
  global (`version_vector.node_<id>`). Los contadores se comparan uno a uno: el
  filtro no depende del orden de las claves. Si la metadata cambió después de la
  lectura, la transacción se aborta y se devuelve `ErrVersionConflict`.
- La conversión entre `NodeID` y `node_<id>` ocurre solo en el adaptador.
- MongoDB replica el documento entre los miembros del Replica Set. La aplicación
  no escribe copias por servidor.
- No se configura `writeConcern` explícito en el código. La escritura usa el
  valor por defecto de la URI o del servidor.

La colección contiene el documento global `cluster_metadata` y los documentos
`node_<id>`.

---

## 8.1 Modo degradado

Si MongoDB no responde al arrancar:

- La elección Bully y los heartbeats siguen funcionando.
- El nodo registra `MongoDB unavailable, retrying in 5s`.
- El proceso reintenta conectar en segundo plano.
- Al reconectar, registra el documento local y activa la persistencia de roles y
  la vigilancia de followers.

No es necesario reiniciar el proceso cuando MongoDB vuelve a estar disponible.

---

## 9. Protocolo gRPC

Servicio `election.v1.ElectionService`, definido en
`proto/election/v1/election.proto`:

| RPC | Request | Response | Uso |
|---|---|---|---|
| `RequestElection` | `RequestElectionRequest` | `RequestElectionResponse` | ELECTION de Bully |
| `AnnounceCoordinator` | `AnnounceCoordinatorRequest` | `AnnounceCoordinatorResponse` | COORDINATOR |
| `ProcessHeartbeat` | `ProcessHeartbeatRequest` | `ProcessHeartbeatResponse` | Heartbeat al coordinador |
| `RegisterNode` | `RegisterNodeRequest` | `RegisterNodeResponse` | Registro de capacidad |

Mensajes compartidos: `Node`, `NodeResources`, `NodeMetadata` (con
`map<string, uint64> version_vector`) y `NodeDocument`.

Generación:

```powershell
buf generate
buf lint
```

Los archivos `election/v1/*.pb.go` se generan. No se editan a mano.

---

## 9.1 Manejo de errores

Errores de configuración: detienen el arranque (`NODE_ID`, `NODE_HOST`,
`PEERS` mal formado o con IDs duplicados, `DB_URI` ausente, puerto ocupado).

Errores de red durante una elección o heartbeat: se interpretan como peer no
disponible. No detienen la ronda.

Errores de gRPC:

- `InvalidArgument`: mensaje mal formado.
- `FailedPrecondition`: nodo desconocido, estado no válido o coordinador no
  autorizado.
- `Unavailable`: persistencia de registro no conectada.

---

## 9.2 Anuncio `COORDINATOR` y registro

El receptor de `AnnounceCoordinator` ejecuta el registro de su documento dentro
de la misma llamada gRPC, antes de responder. El coordinador espera esa
respuesta hasta el timeout de anuncio. Si el registro tarda, el anuncio puede
marcarse como fallido aunque el receptor haya cambiado de rol.

---

## 9.3 Heartbeat y monitor de registro

Dos goroutines reciben el contexto de ejecución:

- Monitor de heartbeats: `ProcessHeartbeatUseCase.Run`.
- Monitor de registro: `RegisterNodeUseCase.Run`. Llama a `Register` en cada
  tick de `HEARTBEAT_MS`.

El monitor de registro envía `RegisterNode` en cada tick cuando el nodo es
follower. Como el coordinador no escribe si nada cambió, no genera escrituras
en MongoDB, pero sí tráfico gRPC.

---

## 9.4 Capacidad física

`system.MachineCapacityReader` obtiene:

- `cpu_cores`: `runtime.NumCPU()`.
- `memory_gb`:
  - Linux: `MemTotal` de `/proc/meminfo`.
  - Windows: `Win32_ComputerSystem.TotalPhysicalMemory` mediante PowerShell.
- `bandwidth_mbps`:
  - Linux: `/sys/class/net/*/speed` (máximo de interfaces no loopback).
  - Windows: `Get-NetAdapter` con estado `Up` (máximo de `LinkSpeed`).

La lectura ocurre una vez por proceso, en el primer `Register`, y queda en
caché. Si la lectura falla, no se guarda nada y se reintenta en el siguiente
ciclo. Si el sistema no informa la velocidad de enlace, `bandwidth_mbps` queda
en `0`.

---

## 10. Dependencias

Módulo Go: `github.com/JhonmaSG/SD_Taller1_Coordinacion_y_datos_del_sistema/src/bully-algorithm`

| Dependencia | Versión | Uso |
|---|---|---|
| `google.golang.org/grpc` | v1.76.0 | Servidor y cliente gRPC |
| `google.golang.org/protobuf` | v1.36.6 | Serialización protobuf |
| `go.mongodb.org/mongo-driver/v2` | v2.4.0 | Driver de MongoDB |

Herramientas de desarrollo (no son dependencias del módulo):

- `buf`: lint y generación de código protobuf.
- `protoc-gen-go` y `protoc-gen-go-grpc`: plugins usados por `buf generate`.

Biblioteca estándar: `context`, `sync`, `time`, `net`, `os`, `os/exec`,
`os/signal`, `runtime`, `bufio`, `strconv`, `strings`.

---

## 11. Validación

Desde `src/bully-algorithm`:

```powershell
gofmt -l .
go vet ./cmd ./internal/...
go test ./...
buf lint
```

El proyecto tiene pruebas unitarias para modelos, vectores, Lamport, elección,
metadata global, actualización de recursos, liveness, configuración y filtros
CAS. Todavía no tiene pruebas de integración contra un Replica Set real ni
pruebas multi-proceso de extremo a extremo.

---

## 12. Estado actual de los flujos

| Flujo | Estado |
|---|---|
| Elección Bully y anuncio `COORDINATOR` | Implementado |
| Heartbeat y reelección al caer el coordinador | Implementado |
| Lectura de capacidad física | Implementado |
| Registro del coordinador en MongoDB | Implementado |
| Registro de followers vía `RegisterNode` | Implementado |
| Reescritura de rol al cambiar de coordinador | Implementado |
| Exclusividad de escritura (casos de uso y repositorio) | Implementado |
| Vector y Lamport globales por cluster | Implementado |
| Documento de recursos sin metadata de concurrencia | Implementado |
| Registro coordinado y estado OFFLINE | Implementado |
| Resolución de conflictos de metadata | Implementado en dominio y cubierta por pruebas unitarias |
| Detección de split-brain, quorum y fencing | No implementado |
| Término (época) de elección | No implementado |
| Reconexión automática a MongoDB | Implementado |
| `writeConcern` explícito | No implementado |
| Pruebas de integración con varios procesos | No implementado |
| TLS / mTLS entre nodos | No implementado |

---

## 13. Limitaciones conocidas

1. **Split-brain**: durante una partición, dos nodos pueden creerse
   coordinadores y escribir. La barrera de escritura solo garantiza que el
   nodo escriba si él cree ser coordinador. Falta término de elección, fencing
   token y quorum.

2. **Vector global**: el vector se almacena en `cluster_metadata`, no en los
   documentos de nodos. La posición que se incrementa es la del coordinador que
   registra cada nuevo liderazgo. Las escrituras de recursos y cambios de
   estado reutilizan la metadata global vigente.

3. **Coordinador caído**: el nuevo coordinador marca `OFFLINE` y `FOLLOWER` al
   antiguo cuando supera el umbral de heartbeats (sección 5.4). Mientras tanto,
   su documento conserva `COORDINATOR` y `ONLINE`.

4. **Detección de followers caídos**: el coordinador los detecta solo por
   ausencia de heartbeats. Un follower aislado de la red pero vivo se marca
   `OFFLINE` igual, y no hay confirmación independiente. El caso de partición
   pertenece al punto 1.

5. **Registro dentro de `AnnounceCoordinator`**: el follower registra su
   documento antes de responder al anuncio. Esto acopla el tiempo de respuesta
   del anuncio al tiempo de MongoDB y del coordinador.

6. **Reintento de MongoDB**: si la base no responde, el proceso reintenta cada
   `5s` y conecta la persistencia cuando MongoDB vuelve a estar disponible.

7. **Sin RPC de inventario**: `UpdateNodeResourcesUseCase` existe, pero ningún
   handler gRPC lo invoca. No hay forma remota de asignar recursos.

8. **Resolución de conflictos sin uso**: `ResolveNodeDocumentConflict` y el
   vector comparado no se usan para reconciliar documentos de distintos
   coordinadores. Hoy el CAS evita la sobrescritura silenciosa, pero no
   reconcilia.

10. **Pruebas**: no hay pruebas de integración contra un Replica Set real ni
    pruebas multi-proceso de extremo a extremo.

11. **Seguridad**: gRPC usa transporte sin TLS. Está pensado para una red
    privada como Tailscale.

### Inicialización del primer coordinador

Si `cluster_metadata` ya existe con `active_coordinator=node_1` pero con reloj y
vector en cero, el siguiente arranque no considera ese documento inicial como
un evento válido. `ClusterMetadataUseCase` detecta esa combinación incompleta y
registra el primer evento del coordinador:

```json
{
  "global_lamport_clock": 1,
  "version_vector": {
    "node_1": 1,
    "node_2": 0,
    "node_3": 0
  },
  "active_coordinator": "node_1",
  "last_modified_by": "node_1"
}
```

### Logs de escritura MongoDB

El adaptador de persistencia registra cada escritura planeada, confirmada o
rechazada. Ejemplos:

```text
MongoDB write planned operation=cluster_metadata_update lamport=4 vector={node_1:1, node_2:2, node_3:1} active_coordinator=node_2 last_modified_by=node_2
MongoDB write committed operation=node_and_cluster_update node=node_3 role=AGENT status=OFFLINE lamport=5 vector={node_1:1, node_2:3, node_3:1} active_coordinator=node_2
MongoDB write failed operation=node_and_cluster_update node=node_3 lamport=5 vector={...} error=node document version conflict
```

El vector se ordena al imprimirlo para que dos logs de la misma metadata sean
comparables aunque los mapas de Go tengan distinto orden interno.

### Write concern

El repositorio configura explícitamente `writeconcern.Majority()` en el
cliente MongoDB. La URI de ejemplo también incluye:

```text
w=majority&wtimeoutMS=5000&retryWrites=true
```

Una escritura solo se considera confirmada cuando la mayoría del Replica Set la
acepta. Si el coordinador de la aplicación queda aislado junto con un miembro
MongoDB y no puede alcanzar la mayoría, la transacción falla y no se incrementa
la metadata global de forma efectiva.
