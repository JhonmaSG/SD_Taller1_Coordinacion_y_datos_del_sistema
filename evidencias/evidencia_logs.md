# Guía rápida para interpretar los logs de evidencias

Este documento orienta la lectura de los logs del experimento de partición de red del Taller 1. Cada archivo `.log` es una captura de un nodo en una fase concreta de la prueba; no representa necesariamente todo el historial de ejecución del nodo.

## 1. Orden de consulta de las evidencias

La evidencia se debe revisar en el siguiente orden:

```text
evidencias/
└── ejecucion-01/
    ├── antes/
    │   ├── nodo1.log
    │   ├── nodo2.log
    │   └── nodo3.log
    ├── durante/
    │   ├── nodo1.log
    │   ├── nodo2.log
    │   └── nodo3.log
    └── despues/
        ├── nodo1.log
        ├── nodo2.log
        └── nodo3.log
```

### Antes

Ruta:

```text
evidencias/ejecucion-01/antes/
```

Muestra el estado inicial del sistema antes de aplicar la partición. Permite comprobar que los nodos están disponibles, que MongoDB funciona correctamente, que existe un coordinador inicial y que el inventario tiene los registros esperados.

### Durante

Ruta:

```text
evidencias/ejecucion-01/durante/
```

Muestra el comportamiento del sistema mientras la comunicación entre nodos está bloqueada. Aquí se buscan los fallos de heartbeat, los nodos que dejan de ser alcanzables, las nuevas elecciones de coordinador, los cambios de estado del inventario y los errores de escritura o disponibilidad.

### Después

Ruta:

```text
evidencias/ejecucion-01/despues/
```

Muestra el estado posterior a la reconexión. Permite comprobar si MongoDB recuperó la comunicación entre sus miembros, si los nodos volvieron a estar `ONLINE`, si se restableció el coordinador esperado y si el inventario convergió a un estado común.

## 2. Contenido de cada archivo `.log`

Todos los logs siguen, en general, el mismo orden de secciones. Para leer rápidamente cualquier archivo se puede revisar cada bloque de arriba hacia abajo.

### Identificación de la captura

El encabezado indica la fase, el nodo que generó la captura y la fecha/hora. Por ejemplo:

```text
===== FASE DURANTE - NODO 1 =====
2026-11-11T03:10:20+00:00
node1
```

Esta información permite relacionar el contenido del archivo con la carpeta y el nodo correspondiente.

### `PUERTOS`

Muestra los puertos en los que está escuchando el proceso `bully-node`. Sirve para comprobar que el servicio del nodo está activo y disponible para recibir comunicaciones.

### `IPTABLES` o `IPTABLES PARTITION`

Muestra las reglas de filtrado de red del nodo. En la fase `antes` permite revisar las reglas normales. En la fase `durante` permite identificar las reglas asociadas al aislamiento de la partición.

Esta sección ayuda a relacionar los errores de comunicación con el bloqueo de tráfico entre nodos, especialmente para gRPC y MongoDB.

### `ESTADO MONGODB`

Presenta el estado de los miembros del replica set `rs-grupo`. Los campos más importantes son:

- `PRIMARY`: miembro que MongoDB considera principal para las escrituras.
- `SECONDARY`: miembro secundario que replica los datos.
- `health`: indica si el miembro está saludable (`1`) o no disponible (`0`).
- `self`: identifica el miembro desde el cual se tomó la captura.
- `lastHeartbeatMessage`: muestra problemas detectados al contactar a otro miembro.
- `syncSource`: indica desde qué miembro se está sincronizando un secundario.

Durante la partición pueden aparecer valores como `(not reachable/healthy)`, `health: 0` o mensajes como `Couldn't get a connection within the time limit`.

### `HELLO`

Es una vista resumida de la topología que conoce MongoDB en ese momento. Permite revisar:

- los miembros conocidos del replica set;
- el `primary` que MongoDB reconoce;
- si el nodo local es `PRIMARY` o `SECONDARY`;
- el nombre del replica set;
- la información de las últimas escrituras.

El `primary` de MongoDB y el coordinador elegido por la aplicación son conceptos relacionados, pero no necesariamente representan el mismo estado durante una partición. Por eso deben revisarse tanto esta sección como el `LOG DE APLICACION`.

### `INVENTARIO`

Contiene los documentos almacenados en la colección `resources`. Los documentos `node_1`, `node_2` y `node_3` muestran, entre otros datos:

- el rol del nodo (`AGENT` o `COORDINATOR`);
- los recursos registrados, como CPU, memoria y ancho de banda;
- el estado lógico del nodo (`ONLINE` u `OFFLINE`).

La comparación de esta sección entre `antes`, `durante` y `despues` permite observar qué nodos fueron marcados como no disponibles y si posteriormente regresaron a un estado operativo.

### `LOG DE APLICACION`

Registra los eventos de coordinación y operación del nodo en orden temporal. Algunos mensajes importantes son:

- `COORDINATOR received` o `COORDINATOR sent`: recepción o envío de anuncios de coordinador.
- `election state`: estado de la elección, como espera o coordinador.
- `heartbeat failed`: el nodo dejó de recibir respuesta del coordinador.
- `starting re-election`: inicio de una nueva elección.
- `node document sync failed`: fallo al sincronizar el documento del nodo.
- `MongoDB write planned`: escritura preparada para el inventario.
- `MongoDB write committed`: escritura confirmada.
- `MongoServerError: not primary`: el miembro contactado no era el principal de MongoDB para esa operación.

Esta sección ayuda a reconstruir la secuencia de eventos: pérdida de comunicación, detección de la falla, elección de otro coordinador, escrituras durante la partición y recuperación posterior.

## 3. Qué buscar en cada fase

### Estado inicial: `antes`

En esta fase se debe confirmar que:

- el proceso del nodo está escuchando en su puerto;
- MongoDB muestra miembros saludables;
- existe un coordinador inicial, que en esta ejecución es el nodo3;
- los nodos del inventario aparecen como `ONLINE`;
- los recursos de CPU, memoria y ancho de banda están registrados.

En los logs iniciales, MongoDB muestra al nodo3 como `PRIMARY` y a los nodos1 y 2 como `SECONDARY`.

### Efectos de la partición: `durante`

En esta fase se deben buscar señales de pérdida de comunicación y cambios de coordinación, como:

- miembros de MongoDB no alcanzables;
- `health: 0`;
- mensajes de timeout o `context deadline exceeded`;
- `heartbeat failed`;
- `starting re-election`;
- elección de un nuevo coordinador;
- nodos marcados como `OFFLINE` en el inventario;
- escrituras planificadas o confirmadas durante la partición;
- errores como `MongoServerError: not primary`.

En esta ejecución se observa que el nodo3 deja de responder, el nodo2 inicia una elección y aparece como coordinador de la aplicación. El inventario también refleja cambios de disponibilidad, especialmente para el nodo3.

### Recuperación: `despues`

En esta fase se debe comprobar que:

- los miembros de MongoDB vuelven a aparecer saludables;
- existe nuevamente un `PRIMARY` definido;
- los nodos regresan a `ONLINE`;
- el coordinador de la aplicación vuelve a anunciarse correctamente;
- los documentos del inventario muestran un estado consistente entre nodos.

En la evidencia posterior se observa la recuperación de MongoDB, el regreso del nodo3 como coordinador y la actualización de documentos del inventario.

## 4. Nota sobre la interpretación de las evidencias

Algunos archivos ubicados en `evidencias/ejecucion-01/despues/` conservan internamente el encabezado `FASE DURANTE`. Para esta guía deben interpretarse como evidencias posteriores porque están dentro de la carpeta `despues` y su contenido muestra la recuperación del sistema.

La fase `durante` evidencia cambios de coordinación y errores de comunicación, pero los logs disponibles no contienen necesariamente mensajes explícitos de detección y resolución de conflictos. Por ello, esta guía describe únicamente lo que puede observarse directamente en las capturas.
