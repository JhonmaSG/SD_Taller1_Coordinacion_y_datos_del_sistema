# Adaptador gRPC

Este paquete implementa los puertos de comunicación del dominio usando el
contrato `proto/election/v1/election.proto`.

- `ElectionTransport` implementa el puerto de salida y envía `ELECTION` y
  `COORDINATOR` a los peers.
- `ElectionServer` implementa el puerto de entrada y delega los mensajes al
  caso de uso Bully.
- Los stubs generados se encuentran en `election/v1`.

La conexión predeterminada usa transporte sin TLS porque los nodos se
comunican dentro de la red privada de Tailscale. Para un despliegue expuesto,
inyectar opciones gRPC con credenciales TLS en `NewElectionTransport`.
