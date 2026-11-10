# Taller 1 — Coordinación y datos del sistema
Sistemas Distribuidos Modernos · Ingeniería en Telemática

## 1. Descripción
Elección de coordinador, inventario replicado (CPU, memoria, ancho de banda), detección de conflictos con vectores de versión y experimento de partición de red.

## 2. Arquitectura
| Nodo | Rol | IP Tailscale | RAM |
|---|---|---|---|
| nodo1 | coordinador/agente | 100.x.x.x | 2 GB |
| nodo2 | coordinador/agente | 100.x.x.x | 2 GB |
| nodo3 | base de datos | 100.x.x.x | 3 GB |

## 3. Requisitos
- Debian 13 / Ubuntu Server 26.04.1 LTS (sin GUI)
- Tailscale, SSH por clave pública (contraseña deshabilitada)
- Base de datos: TODO (MongoDB replica set / Redis maestro-réplica)

## 4. Instalación y ejecución
Todos los comandos de configuración del taller están en [`docs/comandos.md`](docs/comandos.md).

Ver `infra/setup_nodo.md`.
```bash
cp .env.example .env
# TODO: comando de ejecución
```

## 5. Pruebas y experimento de partición
```bash
# TODO: comandos de pruebas
sudo ./scripts/particion/aislar.sh
sudo ./scripts/particion/reconectar.sh
```

## 6. Decisiones técnicas
- Algoritmo de elección: `docs/decisiones/ADR-001-algoritmo-eleccion.md`
- Base de datos: `docs/decisiones/ADR-002-base-de-datos.md`
- Regla de conflictos: `docs/decisiones/ADR-003-regla-conflictos.md`


## 7. Bibliotecas de terceros
| Biblioteca | Versión | Uso | Licencia |
|---|---|---|---|
| TODO | | | |
