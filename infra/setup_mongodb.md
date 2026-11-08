# Instalar y configurar MongoDB 
Se deberá seguir los siguientes pasos para cada uno de los nodos a tener.

### Aclaraciones
Al usar Ubuntu Server 26.04.1 LTS se tiene un conflicto con las versiones de MongoDB 8.x o inferiores, esto se debe a que MongoDB realizar una validación de compatibilidad del kernel de linux a la hora de iniciar el motor de mongoDB. Esto nos obligó a instalar MongoDB 9 para solventar este problema

## 1. Pasos a seguir
1. Instalar dependencias
```bash
sudo apt update
sudo apt install -y gnupg curl
``` 

2. Importar la llave GPG oficial
```bash
curl -fsSL https://pgp.mongodb.com/server-9.asc | \
sudo gpg -o /usr/share/keyrings/mongodb-server-9.gpg \
--dearmor
```

3. Crear el repositorio para Ubuntu 26.04
```bash
echo "deb [ arch=amd64,arm64 signed-by=/usr/share/keyrings/mongodb-server-9.gpg ] https://repo.mongodb.org/apt/ubuntu resolute/mongodb-org/9.0 multiverse" | \
sudo tee /etc/apt/sources.list.d/mongodb-org-9.0.list
```

4. Actualizar repositorios
```bash
sudo apt update
```

5. Comprobar versión disponible
```bash
apt-cache policy mongodb-org
```

6. Validar que en el campo `Candidate` sea 9.x
```bash
 Candidate: 9.0.4
```

7. Instalar MongoDB 9.x
```bash
sudo apt install -y mongodb-org
```

8. Iniciar MongoDB
```bash
sudo systemctl enable mongod
sudo systemctl start mongod
```

9. Comprobar que MongoDB esté corriendo con el comando `systemctl status mongod` por lo que deberá mostrar lo siguiente:
```bash
Active: active (running) ...
```

## 2. Réplica set de 3 miembros
Para implementar la réplica set de 3 miembros en cada nodo, se debe seguir los siguientes pasos 

1. Abrir el archivo de configuración de MongoDB en cada nodo
```bash
sudo nano /etc/mongod.conf
```

2. Buscar la sección `net` y modificar el parámetro `bindIp` para incluir la IP de Tailscale
```YAML
net:
    port: 27017
    bindIp: 127.0.0.1,100.x.x.x # Reemplazar por la IP que sale en la consola de Tailscale del nodo
```

3. Posteriormente, en la sección de replicación, se debe asignar un nombre al set de replicas
```YAML
replication:
    replSetName: "rs-grupo"
```

4. Guardar el archivo y reiniciar el motor de MongoDB en los 3 nodos
```bash
sudo systemctl restart mongod
sudo systemctl status mongod
```

## 3. Iniciar el Set de Réplicas de 3 miembros

1. Conectarse a la terminal de MongoDB
```bash
mongosh
```

2. Inicializar el set de replicas pasando las direcciones IP de Tailscale y el puerto `27017` (solo basta realizarlo con el nodo principal, el resto al conectarse a la consola de MongoDB se inicializarán automáticamente)

```JavaScript
rs.initiate({
  _id: "rs-grupo",
  members: [
    { _id: 0, host: "100.x.x.1:27017" }, // IP del Nodo Principal (3GB)
    { _id: 1, host: "100.x.x.2:27017" }, // IP del Nodo Réplica 1 (2GB)
    { _id: 2, host: "100.x.x.3:27017" }  // IP del Nodo Réplica 2 (2GB)
  ]
})
```

Una vez reemplazados las IP's correspondientes y ejecutado el script de inicialización de las réplicas, nos deberá devolver un resultado JSON con `ok: 1`. Adicionalmente, ya el prompt no será `test>`, sino que cambiará a algo como `rs-grupo [direct: primary] test>` para el nodo principal del a base de datos. Para otros será de la siguiente manera:

```JavaScript
rs-grupo [direct: secondary] test>
```

3. Una vez realizado lo anterior, se verifica el set de replicas para comprobar que los 3 miembros ya están sincronizados y se comunican correctamente.

```JavaScript
rs.status()
```

Esto mostará la salud de los nodos y cual es el primario y cuales los secundarios

## 4. Probar que la replicación funciona
No basta con que los tres servicios estén activos. Deben demostrar que una escritura realizada en el principal termina apareciendo en las réplicas.

1. Insertar información en el PRIMARY
Primero, se debe identificar cuál de los tres nodos tiene el rol PRIMARY mediante:
```JavaScript
rs.status()
```

Una vez identificado, se abre la terminal de MongoDB en dicho nodo:
```bash
mongosh
```

Ejecutar:
```bash
use ejemplo
```

Insertar un registro:
```JavaScript
db.inventario.insertOne(
  {
    nodo: "node-1",
    cpu: 45,
    ram: 60,
    ancho_banda: 100,
    fecha: new Date()
  },
  {
    writeConcern: {
      w: "majority",
      wtimeout: 5000
    }
  }
)
```

Se utiliza writeConcern: { w: "majority" } para garantizar que la escritura sea reconocida por la mayoría de los miembros del Replica Set. En un conjunto de tres miembros, se requiere el reconocimiento de al menos dos.

La respuesta debe mostrar acknowledged: true y el identificador del documento insertado.

2. Consultar la información desde las réplicas SECONDARY
Desde un nodo de lectura ejecutar
```bash
mongosh "mongodb://100.x.x.1:27017,100.x.x.2:27017,100.x.x.3:27017/ejemplo?replicaSet=rs-grupo&readPreference=secondary"
```
Se deben reemplazar las direcciones de ejemplo por las IP reales de los tres nodos.

```JavaScript
db.inventario.find().pretty()
```

Se debe obtener el documento insertado anteriormente. El parámetro readPreference=secondary indica que las consultas deben dirigirse a los nodos secundarios.
Se repite el procedimiento desde el otro nodo SECONDARY para comprobar que ambos recibieron la información.

Resultado esperado: los tres miembros contienen el mismo documento, aunque puede existir un pequeño retraso debido a que la replicación de MongoDB es asíncrona.
