# Seguridad

El panel trata las métricas y los identificadores de sesiones como información privada. El repositorio público contiene código y fixtures sintéticos; las credenciales, bases de datos, capturas y registros de una instalación permanecen fuera del control de versiones.

## Controles

- Token aleatorio de 256 bits, almacenado mediante hash y consumido de forma atómica.
- Caducidad de token y sesión ocho horas después de la emisión.
- Cookie `Secure`, `HttpOnly`, `SameSite=Strict` y prefijo `__Host-`.
- Comprobación de origen, CSRF en cierre de sesión, límite de intentos y cabeceras CSP y `no-store`.
- Backend y gateway ligados a loopback. El gateway bloquea la ingesta y exige un proxy de confianza con host y protocolo exactos.
- Credenciales de ingesta y de gateway independientes de los accesos de usuario.
- Cierre de SSE y eliminación de la vista al caducar o revocar la sesión.
- Fuentes de consumo abiertas en lectura y worker opcional aislable mediante systemd.

La seguridad del host, el proxy HTTPS y sus cuentas de servicio forman parte de la instalación. Conserva las credenciales en archivos con permisos restrictivos y excluye los directorios de datos de cualquier publicación. El administrador debe revisar cambios de tarifas y formatos antes de habilitarlos.

## Informar de una vulnerabilidad

Utiliza el canal privado de avisos de seguridad del repositorio cuando esté disponible. No publiques credenciales, conversaciones, bases de datos ni identificadores privados en incidencias públicas. Incluye una reproducción mínima con datos sintéticos.
