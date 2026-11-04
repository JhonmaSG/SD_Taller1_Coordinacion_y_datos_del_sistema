# Nodo distribuido de inventario

Este módulo contiene el proceso Go de cada nodo del sistema distribuido. La
primera capacidad implementada es la elección de coordinador mediante el
algoritmo del Matón (Bully) sobre gRPC. También incluye la conexión inicial al
Replica Set de MongoDB y el adaptador de persistencia para recursos.

> El proceso inicializa MongoDB y verifica su disponibilidad, pero todavía no
> ejecuta un caso de uso de inventario que lea o escriba recursos. La elección,
> la asignación de recursos, la replicación y los heartbeats son capacidades
> separadas.

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
DB_URI=mongodb://100.x.x.1:27017,100.x.x.2:27017,100.x.x.3:27017/?replicaSet=rs-grupo
DB_NAME=inventory
DB_COLLECTION=resources
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

### Timeout

`TIMEOUT_MS` controla el contexto de la elección inicial. También se propaga a
las llamadas gRPC que realiza esa ronda. Un peer que no responde dentro del
contexto se considera no disponible para esa elección.

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
mongodb.NewResourceRepository()
    │
    ├── mongo.Connect()
    ├── Ping() del Replica Set
    └── repositorio listo para futuros casos de uso
    │
    ▼
grpcadapter.NewElectionServer()
    │
    ▼
net.Listen() + grpc.Server.Serve()
    │
    ▼
election.Start()
```

El servidor gRPC se inicia antes de la elección para que el nodo pueda recibir
solicitudes `ELECTION` mientras participa en su propia ronda.

Durante el apagado:

1. `GracefulStop()` detiene el servidor gRPC.
2. Se cierra el listener TCP.
3. Se desconecta el cliente MongoDB mediante `Close()`.
4. Se liberan los recursos del proceso.

Si falla la configuración, la escucha TCP o el `Ping` de MongoDB, el proceso
termina con un error explícito. Esto evita que un nodo se anuncie como activo
cuando no tiene conectividad con la infraestructura requerida.

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
Una conexión fallida se interpreta como ausencia de respuesta del peer. Si el
nodo local se proclama coordinador y no logra enviar un anuncio, el ID del peer
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
client.Database(DB_NAME).Collection(DB_COLLECTION)
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

- Heartbeats periódicos.
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
