# ADR-002: Base de datos

Opciones consideradas: MongoDB replica set (3 miembros) / Redis maestro-réplica.

## Decisión

Se eligió **MongoDB con replica set de 3 miembros**.

La colección `resources` (configurable con `DB_COLLECTION`) almacena un
documento por recurso de inventario. Cada documento tiene la forma:

```json
{
  "_id": "cpu-nodo1",
  "name": "CPU nodo 1",
  "quantity": 8,
  "metadata": {
    "lamport": 5,
    "version_vector": { "1": 3, "2": 1, "3": 1 },
    "updated_by": 1
  }
}
```

**Modelo de consistencia efectivo:**

| Aspecto | Configuración |
|---|---|
| Escrituras | `writeConcern=majority` — solo se confirman cuando la mayoría del replica set las persiste |
| Lecturas | Desde el primario (comportamiento por defecto del driver de Go) |
| Reintentos | `retryWrites=true` en la URI |
| Timeout de escritura | `wtimeoutMS=5000` en la URI |

La URI de conexión sigue el patrón:
```
mongodb://nodo1:27017,nodo2:27017,nodo3:27017/?replicaSet=rs-grupo&w=majority&wtimeoutMS=5000&retryWrites=true
```

Solo el coordinador elegido puede escribir en MongoDB. Esta restricción se
aplica en el adaptador de persistencia (`ResourceRepository.Save`) verificando
el estado local del algoritmo Bully antes de ejecutar cualquier escritura.

## Justificación

Se eligió MongoDB sobre Redis por las siguientes razones:

- **Modelo de datos flexible**: el inventario combina campos escalares (cantidad)
  con estructuras anidadas (vector de versión como mapa). MongoDB persiste esto
  directamente como BSON sin serialización manual, mientras que Redis requeriría
  codificar el documento como JSON en un valor de cadena o usar estructuras hash
  con claves compuestas.
- **Escrituras con quorum integrado**: `writeConcern=majority` garantiza que una
  escritura solo se confirma cuando al menos 2 de los 3 miembros la han persistido.
  Redis en modo maestro-réplica es asíncrono por defecto y no ofrece garantía de
  durabilidad ante fallo del maestro sin configuraciones adicionales (Redis Cluster
  o Sentinel con `WAIT`).
- **Replica set nativo de 3 miembros**: el replica set de MongoDB reelige
  automáticamente un primario si el actual falla, con quorum de mayoría. Esto
  es coherente con el algoritmo Bully del plano de coordinación.
- **Transacciones multi-documento**: aunque el diseño actual usa un documento por
  recurso, MongoDB permite transacciones ACID que serían necesarias si se
  quisiera actualizar varios recursos de forma atómica en el futuro.

## Consecuencias

- Si el coordinador de la aplicación queda aislado junto con solo un miembro
  MongoDB (minoría), las escrituras con `w=majority` fallan. El nodo queda en
  modo degradado para escrituras pero sigue participando en Bully.
- Las lecturas desde el primario son consistentes pero añaden latencia si el
  primario es el nodo más alejado en la red Tailscale.
- Redis habría ofrecido menor latencia de lectura con `GET` simple, pero a costa
  de una garantía de consistencia más débil.
