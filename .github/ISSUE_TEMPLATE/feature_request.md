---
name: Propuesta de mejora
about: Sugiere una nueva funcionalidad, lector de agente o mejora técnica
title: "[Feat] "
labels: enhancement
assignees: ""
---

## Problema o necesidad

Describe qué caso de uso o problema resuelve esta propuesta.

## Solución propuesta

Explica cómo te gustaría que funcionara y qué partes del sistema afectaría (backend Go, lectores locales, catálogo de tarifas, frontend Svelte 5, despliegue, etc.).

## Compatibilidad con los invariantes

- [ ] Compatible con binario único Go puro (`CGO_ENABLED=0`)
- [ ] Mantiene aritmética decimal exacta y reglas *fail-closed* (si afecta a tarifas)
- [ ] Respeta CSP estricta sin estilos inline y accesibilidad WCAG AA (si afecta a la UI)
- [ ] No expone rutas locales, prompts ni controles de tokenizadores internos
