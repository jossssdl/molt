# molt

[English](README.md) · **Español**

Interfaz de terminal rápida para recuperar espacio en disco de artefactos de desarrollo y cachés locales de IA, con protección inteligente de Git.

molt recorre tus carpetas de proyectos buscando cosas que puedes regenerar (`node_modules`, entornos virtuales de Python, `target` de Rust/Maven, `.next`) y, opcionalmente, cachés de IA de toda la máquina (Hugging Face, pip, blobs huérfanos de Ollama). Cada elemento recibe una insignia de seguridad según el estado del repositorio Git al que pertenece, para que no borres una carpeta de build junto a trabajo sin commitear.

## Qué hace y por qué

**Función principal:** molt encuentra en tu disco las carpetas grandes, desechables y regenerables, te dice si borrar cada una es seguro y te deja eliminarlas con unas pocas teclas.

**El problema.** Cada proyecto que tocas deja carpetas pesadas: `node_modules` (cientos de MB cada una), salidas de build como `.next` y `target`, entornos virtuales de Python, y cachés globales de herramientas de IA (modelos de Hugging Face, wheels de pip, blobs de Ollama que quedaron tras borrar modelos). Con decenas de proyectos viejos esto consume decenas o cientos de GB sin que lo notes. Nada de eso es código fuente: `npm install`, `cargo build` o volver a descargar un modelo lo recupera.

**Por qué no bastan las alternativas.** Borrar a mano obliga a buscar carpeta por carpeta, y un `rm -rf` sobre un proyecto en el que aún trabajas, o en un repo con cambios sin commitear, es fácil de lamentar. Los analizadores de disco genéricos muestran tamaños, pero no saben nada de Git ni de qué es seguro quitar.

**Qué hace molt al respecto:**
1. **Encuentra** solo objetivos conocidos y regenerables, con comprobaciones extra para evitar falsos positivos (por ejemplo, `target` solo cuenta junto a un `Cargo.toml` o `pom.xml`).
2. **Mide** en paralelo, así que escanear un disco entero es rápido y la interfaz nunca se congela.
3. **Consulta Git** del repositorio dueño de cada carpeta (cambios sin commitear, archivos sin seguimiento, commits sin push, sin upstream) y la etiqueta `SAFE`, `DIRTY GIT` o `UNKNOWN`. Los blobs huérfanos de Ollama solo se reportan si se pudieron leer todos los manifests, así que nunca se marca un modelo que sí usas.
4. **Muestra cuán abandonada está** (tiempo desde el último commit o cambio de archivo), de modo que los proyectos viejos sin tocar suben al principio.
5. **Borra con cuidado**: en el modo simulación por defecto no se elimina nada, confirmas de forma explícita, los elementos riesgosos requieren una segunda decisión explícita y cada ruta se revalida justo antes de borrarse. Solo un paquete (`internal/cleaner`) tiene permiso de borrar.

En resumen: *liberar el máximo espacio con el mínimo riesgo de perder trabajo.*

## Características

- **Escáner concurrente**: un recorrido por cada raíz alimenta un pool de workers que miden tamaños; los resultados aparecen en la interfaz mientras el escaneo sigue.
- **Git guard**: cada elemento dentro de un repositorio se revisa una vez por repo: cambios sin commitear, archivos sin seguimiento, commits sin push, rama sin upstream y si Git versiona el propio directorio.
  - `SAFE`: no se perdería nada conocido por Git.
  - `DIRTY GIT`: el repo tiene trabajo sin guardar (el detalle aparece abajo).
  - `UNKNOWN`: no se pudo determinar el estado. Se trata como inseguro.
- **Tabla interactiva**: orden por tamaño o por inactividad (último commit o cambio de archivo), filtros (Todos / Safe / Risky / Dev / AI), selección múltiple con teclado o ratón y una barra con el espacio seleccionado / recuperable / total.
- **Borrado seguro**: simulación (`dry-run`) por defecto. Puedes seleccionar cualquier elemento, pero si la selección incluye `DIRTY GIT` o `UNKNOWN`, molt pregunta si borrar solo los `SAFE` o forzar todos. El cleaner revalida cada ruta justo antes de borrarla.

### Objetivos detectados

| Objetivo | Regla |
| --- | --- |
| `node_modules` | cualquier directorio con ese nombre |
| `.venv` / `venv` | contiene `pyvenv.cfg` |
| `target` | junto a `Cargo.toml` o `pom.xml` |
| `.next` | junto a `package.json` |
| Caché de Hugging Face | `HF_HOME`, `$XDG_CACHE_HOME/huggingface`, `~/.cache/huggingface` (`-ai-caches`) |
| Caché de pip | `PIP_CACHE_DIR`, `~/.cache/pip`, `%LOCALAPPDATA%\pip\Cache` (`-ai-caches`) |
| Blobs huérfanos de Ollama | blobs `sha256-*` que ningún manifest referencia, en `OLLAMA_MODELS` o `~/.ollama/models` (`-ai-caches`) |

La detección de huérfanos no adivina: si faltan los manifests de Ollama o alguno no se puede leer, no reporta nada.

## Instalación

Requiere Go 1.24+ y `git` en el `PATH`.

```sh
go install github.com/jossssdl/molt/cmd/molt@latest
```

O compilar desde el código fuente:

```sh
git clone https://github.com/jossssdl/molt
cd molt
go build -o molt ./cmd/molt
```

## Uso

```sh
molt                           # escanea el directorio actual
molt -root ~/proyectos         # escanea una carpeta (repetible)
molt -root ~/proyectos -ai-caches
molt -list -root ~/proyectos   # imprime una tabla en vez de abrir la TUI
molt -json -root ~/proyectos   # salida para máquinas
molt -dry-run=false -root ~/proyectos   # borra de verdad (aún pide confirmación)
```

| Flag | Descripción |
| --- | --- |
| `-root <dir>` | Directorio a escanear; repítelo para varios. Por defecto, el directorio actual. |
| `-ai-caches` | Incluye también las cachés de IA. |
| `-workers <n>` | Workers de medición (por defecto: número de CPUs). |
| `-list` | Imprime una tabla y termina. |
| `-json` | Imprime JSON y termina. |
| `-dry-run` | `true` por defecto: los borrados solo se simulan. Usa `-dry-run=false` para borrar de verdad. |

### Teclas

| Tecla | Acción |
| --- | --- |
| `↑` `↓` / `j` `k`, `PgUp` `PgDn`, `g` `G` | Moverse |
| `espacio` | Selecciona / deselecciona el elemento actual y baja |
| `a` / `A` / `n` | Selecciona todos los `SAFE` visibles / todo lo visible / limpia la selección |
| `tab` / `shift+tab`, `1`–`5` | Cambiar de filtro |
| `s` / `i` | Ordenar por tamaño / por inactividad |
| `d` o `enter` | Borrar lo seleccionado (pide confirmación) |
| `q` / `esc` | Salir |
| ratón | Clic en una fila para seleccionarla, rueda para desplazarse |

## Desarrollo

```sh
go build ./...
go vet ./...
go test ./...
go test -race ./...   # requiere cgo (un compilador de C)
```

Estructura:

```
cmd/molt            punto de entrada y flags
internal/targets    reglas de detección
internal/scanner    descubrimiento concurrente y medición de tamaño
internal/gitguard   comprobaciones de seguridad con Git
internal/cleaner    el único paquete que borra algo
internal/tui        interfaz con Bubbletea
```

## Licencia

Ver [LICENSE](LICENSE).
