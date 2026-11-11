# Comandos de configuración del taller

Cada comando indica en qué nodo se ejecuta y qué resultado se espera.

## 1. Infraestructura (SSH, Tailscale, conectividad)

Ejecutar en el nodo de origen. Reemplazar `usuario` y las IPs de Tailscale.

```bash
./scripts/verificar_conectividad.sh usuario 100.x.x.2 100.x.x.3
```

- Resultado esperado: ping con respuesta y SSH que imprime el hostname remoto sin pedir contraseña.
- El registro se guarda en `evidencias/E3_conectividad/` (entregable E3).

## 2. Base de datos replicada (MongoDB)

Seguir `infra/setup_mongodb.md` en los tres nodos. Resumen:

```bash
sudo systemctl restart mongod          # en cada nodo, tras editar /etc/mongod.conf
```

Iniciar el Replica Set en el nodo principal con `mongosh`:

```javascript
rs.initiate({ _id: "rs-grupo", members: [ /* IPs de Tailscale, puerto 27017 */ ] })
rs.status()
```

- Resultado esperado: `ok: 1` y un miembro en `PRIMARY`, los otros dos en `SECONDARY`.

## 3. Ejecución de los nodos (coordinador y agentes)

Configurar `src/bully-algorithm/.env` a partir de `.env.example` en cada nodo. Ver la tabla de variables en `src/bully-algorithm/README.md`.

En cada VM:

```bash
cd src/bully-algorithm
go run ./cmd
```

Para pruebas en una sola máquina (Git Bash o WSL, desde la raíz del repositorio):

```bash
bash scripts/levantar_nodos_local.sh
```

- Levanta los nodos 1, 2 y 3 en `127.0.0.1:5001`, `5002` y `5003`.
- Resultado esperado: en los logs aparece `node <id> started` y `MongoDB connection ready`.
- Los logs quedan en `.runtime/local-nodes/logs/`.

Despliegue en las VMs desde una máquina con SSH:

```bash
NODES="usuario1@100.x.x.1 usuario2@100.x.x.2 usuario3@100.x.x.3" bash src/bully-algorithm/scripts/deploy_nodos.sh
```

No copia los `.env`. Cada VM debe tener su propio archivo.

## 4. Pruebas (unitarias y de compilación)

Desde `src/bully-algorithm`:

```bash
gofmt -l .
go vet ./cmd ./internal/...
go test ./...
```

- Resultado esperado: `gofmt` no lista archivos, `go vet` no reporta errores y `go test` termina con `ok` en cada paquete.
- Las pruebas son unitarias. No hay pruebas de integración contra un Replica Set real.

## 5. Experimento de partición (iptables)

Ejecutar en el nodo que se quiere aislar, con `sudo`:

```bash
sudo ./scripts/particion/aislar.sh --interface tailscale0 --grpc-port 5001 --mongo-port 27017
```

- Bloquea el tráfico gRPC y MongoDB sobre la interfaz indicada. SSH (puerto 22) no se bloquea.
- Resultado esperado: el mensaje `Nodo aislado` y la lista de reglas de la cadena `PARTITION`.

Para reconectar:

```bash
sudo ./scripts/particion/reconectar.sh
```

- Resultado esperado: `OK: sin reglas residuales sobre 5000/27017`.

Latencia opcional (100 ms ± 20 ms), con `tc netem`:

```bash
sudo ./scripts/particion/latencia_netem.sh tailscale0 on    # activar
sudo ./scripts/particion/latencia_netem.sh tailscale0 off   # quitar
```

## 6. Recolección de evidencias (E3, E4, E5)

Las capturas de la partición se guardan en `evidencias/ejecucion-01/` con las carpetas `antes`, `durante` y `despues`, un archivo por nodo (`nodo1.log`, `nodo2.log`, `nodo3.log`).

- Cada captura debe incluir: encabezado de fase, puertos, reglas iptables, estado de MongoDB, `hello`, inventario y log de aplicación.
- Para interpretar los archivos, ver `evidencias/evidencia_logs.md`.
- E4 (caída del líder) debe guardarse como logs de los nodos en una carpeta propia.

## 7. Solución de problemas

- **MongoDB no inicia en Ubuntu 26.04:** usar MongoDB 9.x. Las versiones 8.x o anteriores fallan en la validación del kernel. Ver `infra/setup_mongodb.md`.
- **El nodo no conecta a MongoDB:** revisar `DB_URI` y `replicaSet=rs-grupo`. El nodo sigue participando en la elección y reintenta cada 5 s.
- **Quedan reglas iptables tras la prueba:** ejecutar `sudo ./scripts/particion/reconectar.sh` y comprobar el mensaje `OK`.
- **Variable de colección:** el código usa `DB_NODE_COLLECTION` (por defecto `nodes`). El script `levantar_nodos_local.sh` exporta `DB_COLLECTION`, que el código ya no lee. Hay que alinear ambos nombres antes de usar esos datos como evidencia.
