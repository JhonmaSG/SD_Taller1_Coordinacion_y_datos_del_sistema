# ADR-003: Detección y resolución de conflictos

## Detección
Dos escrituras son concurrentes si ningún vector de versión domina al otro. Un reloj de Lamport por sí solo NO detecta concurrencia.

## Resolución
TODO (ejemplo de la guía: gana la mayor marca de Lamport; en empate, el ID de nodo menor).

## Registro
Conteo de conflictos detectados y resueltos.
