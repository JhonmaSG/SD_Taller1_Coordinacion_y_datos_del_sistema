# Modelo de consistencia

El inventario vive en MongoDB con un Replica Set de tres miembros (`rs-grupo`). Ver `docs/decisiones/ADR-002-base-de-datos.md`.

- **Lecturas desde:** el primario. Las lecturas desde secundarios solo se usan en la prueba de replicación de `infra/setup_mongodb.md`, no en el flujo de la aplicación.
- **Confirmación de escritura exigida:** `w=majority`. Una escritura se confirma cuando al menos 2 de los 3 miembros la aceptan. La URI incluye `wtimeoutMS=5000` y `retryWrites=true`.
- **Propagación de actualizaciones:** la replicación de MongoDB es asíncrona hacia los secundarios. La aplicación escribe solo en el primario y no copia datos entre nodos.
- **Escritura única:** solo el coordinador elegido escribe. Esta regla se verifica en los casos de uso y en el repositorio.
- **Versionado:** cada cambio de liderazgo incrementa un reloj de Lamport y un vector de versión globales, guardados en el documento `cluster_metadata`. El vector se compara con CAS para detectar actualizaciones concurrentes. Ver `docs/decisiones/ADR-003-regla-conflictos.md`.

## Comportamiento en minoría

Si el coordinador queda aislado con un solo miembro de MongoDB, sus escrituras con `w=majority` fallan por timeout. El nodo sigue participando en la elección Bully, pero no confirma cambios de inventario hasta recuperar la mayoría.

## Límite conocido

Dos coordinadores pueden escribir al mismo tiempo durante una partición. Esto sucede porque el sistema no tiene término de elección ni fencing token. El CAS evita sobrescrituras silenciosas, pero la reconciliación automática de documentos divergentes no está integrada en el flujo de la aplicación.
