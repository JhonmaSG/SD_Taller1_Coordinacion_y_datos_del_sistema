# Nodo distribuido de inventario

Este módulo contiene el proceso Go de cada nodo del sistema distribuido. La
primera capacidad implementada es la elección de coordinador mediante el
algoritmo del Matón (Bully) sobre gRPC. También incluye la conexión inicial al
Replica Set de MongoDB y el adaptador de persistencia para recursos.

> El proceso inicializa MongoDB y verifica su disponibilidad, pero todavía no
> ejecuta un caso de uso de inventario que lea o escriba recursos. La elección,
> la detección de caída del coordinador, la asignación de recursos y la
> replicación son capacidades separadas.

## 1. Cómo se ejecuta el proceso

El módulo tiene su propio `go.mod`, por lo que los comandos se ejecutan desde
`src/bully-algorithm`:

```powershell
Set-Location src/bully-algorithm
Copy-Item ..\..\.env.example .env

go mod download
go run ./cmd
```

Cada nodo debe tener su propio archivo `.env`. El archivo se carga de manera
opcional al iniciar. Si una variable ya existe en el entorno del sistema, esa
variable tiene prioridad sobre el archivo.

El punto de entrada es:

```text
cmd/main.go
```

`main.go` ejecuta el ensamblaje de dependencias y después mantiene vivo el
servidor gRPC hasta recibir `Ctrl+C` o `SIGTERM`.

## 2. Configuración

Las variables soportadas son:

```env
NODE_ID=1
NODE_HOST=100.x.x.1
NODE_PORT=5000
PEERS=2@100.x.x.2:5000,3@100.x.x.3:5000
TIMEOUT_MS=3000
HEARTBEAT_MS=1000
DB_URI=mongodb://100.x.x.1:27017,100.x.x.2:27017,100.x.x.3:27017/?replicaSet=rs-grupo
DB_NAME=inventory
DB_NODE_COLLECTION=nodes
```

### Identidad y dirección

- `NODE_ID` es el identificador numérico usado por Bully.
- `NODE_HOST` es la dirección local de Tailscale o la dirección de escucha.
- `NODE_PORT` tiene `5000` como valor predeterminado.
- `LISTEN_ADDRESS` puede sobrescribir la combinación `NODE_HOST:NODE_PORT`.

### Peers

`PEERS` utiliza el formato explícito:

```text
<id>@<host>:<port>,<id>@<host>:<port>
```

Ejemplo:

```env
PEERS=2@100.x.x.2:5000,3@100.x.x.3:5000
```

El ID debe formar parte de la configuración porque el algoritmo no puede
inferir la prioridad de un nodo a partir de su dirección IP. Los IDs deben ser
positivos, únicos y no pueden repetir el ID local.

### Heartbeat

`HEARTBEAT_MS` define cada cuánto un nodo seguidor consulta al coordinador
actual. Si el heartbeat falla, el nodo inicia una nueva elección con Bully.
El valor predeterminado es `1000` milisegundos.

## 3. Flujo de arranque de `main.go`

El flujo de composición es:

```text
.env opcional
    │
    ▼
config.LoadFromEnv()
    │
    ├── model.Node local
    ├── []model.Node peers
    ├── dirección de escucha
    ├── timeout de elección
    └── URI y nombres de MongoDB
    │
    ▼
config.NewStaticMembership()
    │
    ▼
grpcadapter.NewElectionTransport()
    │
    ▼
usecase.NewBullyElectionUseCase()
    │
    ▼
net.Listen() + grpc.Server.Serve()
    │
    ▼
election.Start()
    │
    ▼
MongoDB (modo operativo o degradado)
    │
    ├── mongo.Connect()
    ├── Ping() del Replica Set
    └── repositorio listo si la BD responde
```

El servidor gRPC se inicia antes de la elección para que el nodo pueda recibir
solicitudes `ELECTION` mientras participa en su propia ronda. La elección no
espera a que MongoDB esté disponible: la coordinación es el plano de control y
la base de datos es el plano de datos. Si MongoDB no responde, el nodo continúa
en modo degradado y las operaciones de inventario quedan deshabilitadas hasta
que exista una estrategia de reconexión.

1. `GracefulStop()` detiene el servidor gRPC.
2. Se cierra el listener TCP.
3. Se desconecta el cliente MongoDB mediante `Close()`.
4. Se liberan los recursos del proceso.

Si falla la configuración o la escucha TCP, el proceso termina con un error
explícito. Si falla el `Ping` de MongoDB, el proceso registra una advertencia y
continúa con el plano de coordinación activo. Esto permite que el nodo
participe en Bully aunque el Replica Set esté incompleto o temporalmente sin
primary. Las asignaciones de inventario deben permanecer deshabilitadas hasta
que MongoDB vuelva a estar disponible.

## 4. Arquitectura de carpetas

```text
src/bully-algorithm/
├── cmd/
│   └── main.go
├── proto/
│   └── election/v1/election.proto
├── election/v1/
│   ├── election.pb.go
│   └── election_grpc.pb.go
├── internal/
│   ├── domain/
│   │   ├── model/
│   │   └── usecase/
│   ├── ports/
│   └── adapters/
│       ├── config/
│       ├── grpc/
│       └── mongodb/
├── buf.yaml
├── buf.gen.yaml
├── go.mod
└── go.sum
```

### Dominio: `internal/domain`

El dominio no importa MongoDB, gRPC ni paquetes de infraestructura.

Modelos principales:

- `model/node.go`: identidad, dirección lógica y prioridad de nodos.
- `model/election.go`: estados y resultados de la elección.
- `model/resource.go`: recurso de inventario.
- `model/metadata.go`: timestamp de Lamport, vector de versiones y nodo
  actualizador.
- `model/lamport.go`: reloj lógico de Lamport.
- `model/version_vector.go`: comparación causal, concurrencia y fusión de
  versiones.

El caso de uso de elección está en:

```text
internal/domain/usecase/bully_election_usecase.go
```

`BullyElectionUseCase` depende únicamente de interfaces de puertos. Su
responsabilidad es decidir cuándo preguntar a nodos superiores, cuándo
proclamarse coordinador y cuándo aceptar un anuncio.

### Puertos: `internal/ports`

Los puertos definen contratos estables entre el dominio y la infraestructura:

```go
type MembershipPort interface {
    LocalNode() model.Node
    Peers() []model.Node
}
```

Este contrato permite que la membresía provenga de variables de entorno, un
servicio de descubrimiento o una configuración remota.

```go
type ElectionTransportPort interface {
    RequestElection(context.Context, model.Node, model.Node) (bool, error)
    AnnounceCoordinator(context.Context, model.Node, model.Node, model.Node) error
}
```

El caso de uso no sabe que el transporte final es gRPC.

```go
type ResourceRepositoryPort interface {
    FindByID(context.Context, string) (model.Resource, error)
    Save(context.Context, model.Resource) error
}
```

Este último contrato prepara los casos de uso de inventario para que no
conozcan MongoDB.

### Adaptadores: `internal/adapters`

#### Configuración

`internal/adapters/config/env.go` implementa:

- Lectura y validación de variables de entorno.
- Carga opcional de `.env` con la biblioteca estándar.
- Parsing de `PEERS`.
- `StaticMembership`, implementación de `MembershipPort`.

#### gRPC

`internal/adapters/grpc/election_transport.go` implementa el puerto de salida.
Abre una conexión al peer, serializa el mensaje protobuf y ejecuta el RPC.

`internal/adapters/grpc/election_server.go` implementa el puerto de entrada.
Recibe mensajes gRPC, valida su contenido, los convierte a modelos de dominio
y delega en `BullyElectionUseCase`.

#### MongoDB

`internal/adapters/mongodb/resource_repository.go` implementa el repositorio
para recursos. Se encarga de:

- Crear el cliente MongoDB.
- Ejecutar `Ping` durante el arranque.
- Buscar recursos por ID.
- Insertar o reemplazar recursos con `ReplaceOne` y `upsert`.
- Convertir el vector de versiones entre Go y BSON.
- Cerrar el pool de conexiones durante el apagado.

## 5. Flujo del algoritmo Bully

Supongamos tres nodos:

```text
Nodo 1
Nodo 2
Nodo 3
```

El nodo con el ID más alto tiene mayor prioridad para ser coordinador.

### 5.1 Inicio de una ronda

`main.go` llama:

```go
result, err := election.Start(ctx)
```

`BullyElectionUseCase.Start` hace lo siguiente:

1. Obtiene el nodo local y los peers mediante `MembershipPort`.
2. Valida IDs y elimina el nodo local de la lista de peers.
3. Filtra los peers cuyo ID es mayor que el local.
4. Envía un `ELECTION` concurrentemente a cada peer superior.
5. Cuenta las respuestas afirmativas.

Si el nodo local es el nodo 1, consulta a los nodos 2 y 3. Si es el nodo 3,
no consulta a nadie porque no existen IDs superiores.

### 5.2 Mensaje `ELECTION`

El contrato está en:

```text
proto/election/v1/election.proto
```

```protobuf
rpc RequestElection(RequestElectionRequest)
    returns (RequestElectionResponse);
```

El cliente envía:

```protobuf
RequestElectionRequest {
  sender: {
    id: 1,
    address: "100.x.x.1:5000"
  }
}
```

El servidor remoto recibe el mensaje en:

```go
ElectionServer.RequestElection()
```

Después delega en:

```go
BullyElectionUseCase.HandleElection()
```

Si el nodo receptor tiene un ID mayor que el solicitante:

1. Responde `acknowledged=true`.
2. Inicia su propia elección en segundo plano.

Si no tiene mayor prioridad, responde `acknowledged=false`.

### 5.3 Nodo superior disponible

Si el nodo local recibe al menos una respuesta afirmativa, pasa a:

```text
ElectionWaitingForCoordinator
```

Eso significa que otro nodo con mayor prioridad está activo y debe continuar
la elección.

### 5.4 Ningún nodo superior disponible

Si ningún nodo superior responde, el nodo local se proclama coordinador:

```text
ElectionCoordinator
```

Luego envía `COORDINATOR` a todos los peers conocidos, no únicamente a los
nodos inferiores.

### 5.5 Mensaje `COORDINATOR`

El contrato define:

```protobuf
rpc AnnounceCoordinator(AnnounceCoordinatorRequest)
    returns (AnnounceCoordinatorResponse);
```

El receptor procesa el anuncio en:

```go
ElectionServer.AnnounceCoordinator()
```

El caso de uso verifica que el coordinador pertenezca a la membresía conocida.
Si es válido:

- El nodo remoto pasa a `ElectionFollowing`.
- Se guarda el coordinador recibido.
- Se responde `accepted=true`.

Si el anuncio no es válido, se devuelve un error gRPC de tipo
`FailedPrecondition`.

## 6. Manejo de errores

Los errores de configuración detienen el arranque:

- `NODE_ID` ausente o inválido.
- `NODE_HOST` vacío.
- IDs de peers duplicados.
- Entrada `PEERS` sin formato `<id>@<address>`.
- `DB_URI` ausente.
- MongoDB no disponible.
- Puerto local ocupado.

Los errores de comunicación durante una elección no detienen toda la ronda.
Una conexión fallida se interpreta como ausencia de respuesta del peer. El
vencimiento normal de `TIMEOUT_MS` también se interpreta como ausencia de
respuesta, no como un error fatal de la elección. Por ejemplo, si el nodo 1
inicia con los nodos 2 y 3 apagados, espera el timeout, se proclama coordinador
y continúa ejecutando. Si uno de los nodos superiores responde `acknowledged=true`,
el nodo local espera el anuncio del coordinador porque existe evidencia de un
nodo de mayor prioridad activo.

Si el nodo local se proclama coordinador y no logra enviar un anuncio, el ID del peer
se guarda en `AnnouncementFailures`.

Las solicitudes gRPC mal formadas se rechazan con códigos adecuados:

- `InvalidArgument` para mensajes inválidos.
- `FailedPrecondition` para nodos desconocidos o estados no válidos.

## 7. Conexión con MongoDB

La infraestructura documentada en `infra/setup_mongodb.md` usa el Replica Set:

```text
rs-grupo
```

Por eso la URI debe utilizar el mismo nombre:

```text
mongodb://100.x.x.1:27017,100.x.x.2:27017,100.x.x.3:27017/?replicaSet=rs-grupo
```

El flujo de conexión es:

```text
DB_URI
  │
  ▼
mongo.Connect(options.Client().ApplyURI(DB_URI))
  │
  ▼
client.Ping(ctx, nil)
  │
  ▼
client.Database(DB_NAME).Collection(DB_NODE_COLLECTION)
```

El driver mantiene un pool de conexiones y MongoDB decide qué miembro del
Replica Set atiende cada operación. La aplicación no debe asumir que el nodo
coordinador de Bully es el primary de MongoDB. Son elecciones distintas:

```text
Bully:
    coordina las asignaciones y operaciones de la aplicación.

MongoDB Replica Set:
    elige el primary que atiende las operaciones de escritura de MongoDB.
```

La implementación actual persiste los metadatos del recurso como:

```json
{
  "_id": "cpu-nodo-1",
  "name": "CPU",
  "quantity": 4,
  "metadata": {
    "lamport": 12,
    "version_vector": {
      "1": 7,
      "2": 3
    },
    "updated_by": 1
  }
}
```

El caso de uso de inventario todavía debe decidir cuándo llamar a `Save` y cómo
resolver una versión concurrente antes de persistirla.

## 8. Librerías utilizadas

### gRPC y protobuf

```text
google.golang.org/grpc v1.76.0
google.golang.org/protobuf v1.36.6
```

Se utilizan para:

- Crear el servidor gRPC.
- Crear el cliente gRPC.
- Serializar mensajes protobuf.
- Registrar el servicio `ElectionService`.

Los stubs se generan usando:

```text
protoc-gen-go
protoc-gen-go-grpc
```

La configuración de generación está en:

```text
buf.yaml
buf.gen.yaml
```

Para regenerar los archivos:

```powershell
buf generate
```

Los archivos generados son:

```text
election/v1/election.pb.go
election/v1/election_grpc.pb.go
```

No deben editarse manualmente. El archivo fuente es el `.proto`.

### MongoDB

```text
go.mongodb.org/mongo-driver/v2 v2.4.0
```

Se utiliza para crear el cliente, ejecutar `Ping`, consultar recursos y guardar
reemplazos con `upsert`.

### Biblioteca estándar de Go

La aplicación utiliza la biblioteca estándar para:

- `context`: cancelación y timeouts.
- `net`: listener TCP.
- `os` y `os/signal`: entorno y apagado ordenado.
- `sync`: protección del estado del caso de uso.
- `time`: timeouts.
- `encoding` no es necesario para el protocolo de elección porque se utiliza
  protobuf.

## 9. Comandos de validación

Desde `src/bully-algorithm`:

```powershell
buf lint
buf generate
go test ./cmd ./election/v1 ./internal/...
go vet ./internal/...
```

## 10. Limitaciones actuales

Esta implementación deja preparados los componentes, pero todavía faltan:

- Reintento automático si llega `OK` pero no llega `COORDINATOR`.
- Caso de uso de asignación de recursos.
- Sincronización entre réplicas.
- Adaptación del estado del coordinador a la autorización de asignaciones.
- Pruebas de integración con tres procesos reales.
- TLS mTLS para gRPC fuera de la red privada.
- Elección formal de MongoDB frente a Redis en `ADR-002`.

La elección Bully ya está separada de esos componentes. Esto permite agregar
los casos de uso posteriores sin cambiar el contrato protobuf ni introducir
MongoDB dentro del dominio.

## 11. Ejecutar tres nodos en local

Para probar Bully sin levantar tres máquinas, el repositorio incluye:

```text
scripts/levantar_nodos_local.sh
```

El script compila una única vez el binario y levanta tres procesos con esta
configuración:

```text
Nodo 1 → 127.0.0.1:5001
Nodo 2 → 127.0.0.1:5002
Nodo 3 → 127.0.0.1:5003
```

Cada proceso recibe una membresía completa y el nodo 3 tiene la prioridad más
alta. El script reutiliza `DB_URI`, `DB_NAME`, `DB_NODE_COLLECTION`, `TIMEOUT_MS` y
`HEARTBEAT_MS` desde `src/bully-algorithm/.env`.

Desde Git Bash o WSL, ejecutar en la raíz del repositorio:

```bash
bash scripts/levantar_nodos_local.sh
```

Los logs se escriben en:

```text
.runtime/local-nodes/logs/node1.log
.runtime/local-nodes/logs/node2.log
.runtime/local-nodes/logs/node3.log
```

Presionar `Ctrl+C` en la terminal del script detiene los tres procesos. El
binario y los logs quedan dentro de `.runtime/`, que está excluido mediante
`.gitignore`.

En Windows PowerShell sin Git Bash ni WSL, el archivo `.sh` no se puede ejecutar
directamente. En ese caso se puede instalar Git Bash o usar las tres terminales
PowerShell con `go run ./cmd` y las variables `NODE_ID`, `NODE_PORT` y `PEERS`
configuradas por proceso.

## 12. Detección de caída del coordinador

El caso de uso `ProcessHeartbeatUseCase` está en:

```text
internal/domain/usecase/process_heartbeat_usecase.go
```

Cada nodo supervisa periódicamente al coordinador que conoce. El intervalo se
configura con `HEARTBEAT_MS` y usa `TIMEOUT_MS` como límite de cada heartbeat y
de la elección posterior.

El flujo es:

```text
Nodo seguidor
    │
    ├── ProcessHeartbeat ──> Coordinador
    │
    ├── alive=true
    │       └── continúa supervisando
    │
    └── timeout/error
            │
            └── inicia una nueva elección Bully
```

El RPC se define en `proto/election/v1/election.proto`:

```protobuf
rpc ProcessHeartbeat(HeartbeatRequest) returns (HeartbeatResponse);
```

Si el coordinador 2 se apaga mientras el nodo 1 está activo:

1. El nodo 1 deja de recibir respuestas.
2. El heartbeat agota su timeout.
3. El nodo 1 inicia `BullyElectionUseCase.Start`.
4. Los nodos 2 y 3 que no respondan se consideran caídos.
5. El nodo 1 se proclama coordinador si es el nodo activo con mayor ID.
6. El nodo 1 anuncia `COORDINATOR` a los peers disponibles.

El monitor se inicia desde `cmd/main.go` después de la elección inicial y se
detiene mediante el mismo contexto de apagado ordenado del servidor gRPC.

## 13. Documentos por nodo y control de concurrencia

La persistencia de nodos usa la colección configurada por
`DB_NODE_COLLECTION`, cuyo valor predeterminado es `nodes`. Cada proceso
registra únicamente su propio documento lógico; MongoDB Replica Set replica ese
documento entre sus miembros.

Ejemplo persistido:

```json
{
  "_id": "node_1",
  "node_role": "COORDINATOR",
  "resources": {
    "cpu_cores": 2,
    "memory_gb": 3,
    "bandwidth_mbps": 1000
  },
  "status": "ONLINE",
  "metadata": {
    "lamport_clock": 15,
    "version_vector": {
      "node_1": 6,
      "node_2": 2,
      "node_3": 2
    },
    "last_modified_by": "node_1"
  }
}
```

Los modelos de dominio están en:

```text
internal/domain/model/node_document.go
internal/domain/model/lamport.go
internal/domain/model/version_vector.go
internal/domain/model/metadata.go
```

El caso de uso de actualización está en:

```text
internal/domain/usecase/update_node_resources_usecase.go
```

Antes de modificar recursos, el caso de uso:

1. Verifica mediante `CoordinatorView` que el nodo local sea el coordinador.
2. Lee el documento actual mediante `NodeDocumentRepository`.
3. Observa el Lamport remoto y avanza el reloj para el evento local.
4. Incrementa el contador `version_vector[node_id]`.
5. Actualiza `last_modified_by`.
6. Intenta el reemplazo atómico con el metadata leído como precondición.
7. Reintenta conflictos de versión limitadamente, sin sobrescribir una
   modificación concurrente a ciegas.

El puerto es:

```text
internal/ports/node_document_repository.go
```

Y el adaptador MongoDB es:

```text
internal/adapters/persistence/mongo_repository.go
```

El adaptador filtra la operación por `_id` y por:

```text
metadata.lamport_clock
metadata.version_vector
metadata.last_modified_by
```

Si esos valores cambiaron desde la lectura, MongoDB no actualiza el documento y
el adaptador devuelve `ErrVersionConflict`.

El dominio no contiene imports de MongoDB ni tags BSON. El mapeo entre
`NodeID(1)` y `node_1` ocurre exclusivamente en el adaptador.

El repositorio anterior de recursos individuales se conserva para compatibilidad
con código legado, pero el flujo de documentos por nodo utiliza
`MongoNodeDocumentRepository` y la colección `nodes`.

## 14. Detección de capacidad física

Los valores de `resources` no se configuran en `.env`. Al registrar el nodo,
`RegisterNodeUseCase` utiliza `MachineCapacityReader` para leer la capacidad
del host:

```text
internal/ports/machine_capacity.go
internal/adapters/system/machine_capacity.go
```

La implementación actual obtiene:

- `cpu_cores`: `runtime.NumCPU()`, es decir, las CPUs lógicas/vCPU visibles
  para el proceso.
- `memory_gb`: memoria física total. En Linux se lee `MemTotal` de
  `/proc/meminfo`; en Windows se consulta `Win32_ComputerSystem` mediante
  PowerShell.
- `bandwidth_mbps`: máxima velocidad de enlace de las interfaces activas. En
  Linux se lee `/sys/class/net/*/speed`; en Windows se consulta `Get-NetAdapter`.

Si el sistema operativo no expone la velocidad de enlace, `bandwidth_mbps`
queda en `0` y la aplicación continúa. La velocidad real de Internet o de
Tailscale no puede inferirse siempre de forma fiable desde el host; la lectura
corresponde a la capacidad de enlace que reporta la interfaz.

La detección ocurre al iniciar el proceso y se registra mediante un upsert CAS
en `DB_NODE_COLLECTION`:

```text
DB_NODE_COLLECTION=nodes
```

En los logs debe aparecer:

```text
machine capacity registered cpu_cores=... memory_gb=... bandwidth_mbps=...
```

Si el nodo ya tenía un documento, la capacidad detectada reemplaza los valores
anteriores durante el registro. Para detectar cambios de CPU, RAM o interfaces
de red se debe reiniciar el proceso. No se agregan `CPU_CORES`, `MEMORY_GB` ni
`BANDWIDTH_MBPS` al `.env`.

## 15. Registro de followers a través del coordinador

El registro de capacidad respeta la exclusividad de escritura del coordinador.
El follower obtiene CPU, memoria y bandwidth localmente, pero no persiste su
documento directamente en MongoDB.

```text
Follower
  │ mide capacidad
  │ RegisterNode gRPC
  ▼
Coordinador
  │ fusiona Lamport y vector del documento
  │ incrementa su propia versión de escritura
  ▼
MongoDB: nodes/node_<id>
```

El RPC utilizado es:

```protobuf
rpc RegisterNode(RegisterNodeRequest) returns (RegisterNodeResponse);
```

Si el proceso inicia antes de conocer al coordinador, el caso de uso reintenta
periódicamente hasta recibir un anuncio `COORDINATOR`. Un proceso que es
coordinador registra su propio documento localmente. Un follower solo solicita
al coordinador que registre o actualice su documento.

El vector es independiente por documento. Durante el registro de `node_2`, por
ejemplo, el coordinador fusiona la metadata enviada por `node_2` con el vector
actual del documento `node_2` y registra su propia escritura. No se copia un
vector global a todos los documentos.

## 16. Exclusividad de escritura del coordinador

La exclusividad se aplica en dos niveles:

1. Los casos de uso verifican `CoordinatorView.IsLocalCoordinator()` antes de
   registrar nodos o actualizar recursos.
2. `MongoNodeDocumentRepository.UpdateAtomic` también verifica esa condición
   antes de ejecutar `ReplaceOne`. Por tanto, una llamada directa al repositorio
   desde un follower devuelve `ErrNotCoordinator`.

El repositorio legacy de recursos aplica la misma barrera en `Save`. La conexión
MongoDB y las lecturas no están restringidas; la restricción se aplica a las
operaciones de escritura.

Esta protección representa la autoridad local observada por Bully. La
prevención de dos coordinadores simultáneos durante una partición requiere
fencing/quorum y queda fuera de esta etapa.
