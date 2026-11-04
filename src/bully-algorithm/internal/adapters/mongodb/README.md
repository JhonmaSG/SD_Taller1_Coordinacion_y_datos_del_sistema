# Adaptador MongoDB

`ResourceRepository` implementa `ports.ResourceRepositoryPort` usando el driver
oficial de MongoDB para Go.

La URI debe incluir el Replica Set configurado en infraestructura, por ejemplo:

```text
mongodb://100.x.x.1:27017,100.x.x.2:27017,100.x.x.3:27017/?replicaSet=rs-grupo
```

Al iniciar, el adaptador crea el cliente y ejecuta `Ping`. Las operaciones de
inventario usan `resources` como colección por defecto y guardan el vector de
versiones como un mapa cuyos nombres de campo son los IDs de nodo.
