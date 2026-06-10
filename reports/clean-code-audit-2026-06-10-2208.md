# Clean-Code Audit Report

- **Project:** grizzly
- **Path:** `/home/gverdugo/Projects/grizzly`
- **Date:** 2026-06-10 22:08
- **Auditor:** Claude Code — `dev:clean-code-audit` skill
- **Lens:** Clean-Code (naming · structure · duplication · comment hygiene · dead code · magic constants · god objects · abstraction leaks · style consistency · complexity coverage)
- **Scope:** 23 files analyzed · ~4 034 LOC (approx.)

---

## Executive Summary

**Overall rating:** healthy

grizzly es una librería de dataframes en Go construida como proyecto de aprendizaje, y la auditoría confirma que ese objetivo no ha ido en detrimento de la artesanía: nombres honestos, funciones de responsabilidad única, comentarios que explican el porqué y una cobertura de tests amplia recién incorporada. No hay hallazgos CRITICAL ni HIGH. La conclusión más relevante es estructural: los tres loaders y `Where` repiten un mismo patrón "collect-then-finish"/gather por tipo concreto que conviene unificar antes de añadir un cuarto dtype, y la interfaz `Column` carece de un accessor de validity que ya ha forzado un escape hatch (`columnValidity`).

### Top findings

Ordered by severity, then by blast radius (how many people or flows are affected).

| #   | Severity | Category          | Finding                                                                  |
| --- | -------- | ----------------- | ------------------------------------------------------------------------ |
| 1   | MEDIUM   | duplication       | Duplicación estructural del patrón "collect-then-finish" en los loaders  |
| 2   | MEDIUM   | abstraction-leak  | `columnValidity` señala una ausencia en la interfaz `Column`             |
| 3   | MEDIUM   | duplication       | `Where` repite el gather por tipo en vez de reutilizar `gatherRows`      |
| 4   | MEDIUM   | naming            | El parámetro `method string` en `compare` solo alimenta mensajes de error |
| 5   | LOW      | duplication       | `cmpBoolRows` duplica la lógica null-first de `cmpRows`                  |

### Severity counts

| CRITICAL  | HIGH      | MEDIUM    | LOW       | INFO      |
| --------- | --------- | --------- | --------- | --------- |
| 0         | 0         | 4         | 3         | 2         |

---

## Findings

### [MEDIUM] Duplicación estructural del patrón "collect-then-finish" en los tres loaders

- **Location:** `/home/gverdugo/Projects/grizzly/from_csv.go:72–139`, `/home/gverdugo/Projects/grizzly/from_json.go:55–127`
- **Category:** duplication
- **Description:** Ambos loaders construyen a mano un par de slices paralelos (`fills []func(...)`, `finish []func() (Column, error)`) usando el mismo esquema de closures que capturan `values`, `valid` y `name`. El bloque `switch field.Type { case Float64 / String / Bool }` aparece en cada loader con diferencias sólo en la función de parse (línea activa vs. token JSON). La estructura del final del loop —construir `cols`, llamar a cada `finish` y pasar a `NewDataframe`— es idéntica en ambos archivos (from_csv.go:157–165, from_json.go:172–180).
- **Why it bites:** Añadir un cuarto tipo (p. ej. `Int64`) requiere editar tres lugares en paralelo (from_csv, from_json y from_structs), con riesgo de divergencia silenciosa. Si se añade una semántica de null diferente para `Bool` en CSV pero se olvida replicarla en JSON, los tests solo cubren el loader que se modificó.
- **Fix:** 1. Extraer un tipo `columnBuilder` con métodos `append(rawValue, isNull)` y `finish() (Column, error)`. 2. Hacer que `FromCSVReader` y `FromJSONReader` construyan una `[]columnBuilder` a partir del schema, delegando el "cómo parseo este tipo" a cada builder. 3. El loop final queda como un helper compartido `buildDataframe(builders []columnBuilder) (Dataframe, error)`. Riesgo bajo: los tests de loaders lo cubrirían completamente.
- **Effort:** M
- **Confidence:** high

### [MEDIUM] El parámetro `method string` en `compare` filtra hacia los mensajes de error en lugar de dirigir comportamiento

- **Location:** `/home/gverdugo/Projects/grizzly/mask.go:172`
- **Category:** function-length / naming
- **Description:** La función interna `compare(method string, op compareOp, name string, value any)` recibe el nombre del método público (`"Eq"`, `"Lt"` etc.) exclusivamente para incluirlo en los mensajes de error. El parámetro no altera la lógica de ninguna rama. Cada mensaje lo usa así: `fmt.Errorf("%w: %s on float64 column %q ...", ErrTypeMismatch, method, ...)`.
- **Why it bites:** Cualquier refactor que renombre un método público (p. ej. `Lt` → `LessThan`) obliga a actualizar cada callsite de `compare` para pasar el string correcto; el compilador no avisa si el string y el nombre real divergen. Es una fuente silenciosa de mensajes de error engañosos.
- **Fix:** Eliminar el parámetro `method` de `compare`. Los mensajes de error ya contienen `op` (un `compareOp` con nombre) y el nombre de la columna; añadir `aggNames`-style lookup para `compareOp` si se quiere el nombre legible en los errores, o simplemente omitirlo. El sitio de llamada queda `d.compare(opEq, name, value)`.
- **Effort:** S
- **Confidence:** high

### [MEDIUM] `Where` contiene un bloque de gather por tipo repetido tres veces sin abstracción

- **Location:** `/home/gverdugo/Projects/grizzly/mask.go:296–365`
- **Category:** duplication
- **Description:** El switch de tipos dentro de `Where` materializa los valores supervivientes con un patrón idéntico para `Float64Column`, `StringColumn` y `BoolColumn`: (1) reservar slice de valores, (2) reservar `valid []bool` si hay bitmap, (3) iterar `setBits(keep)`, (4) llamar al constructor `WithNulls` o al sin-nulls según `valid`. Los tres cases son estructuralmente iguales; sólo cambia el tipo concreto del slice.
- **Why it bites:** `gatherRows` en `column.go:208–239` ya hace exactamente lo mismo para GroupBy y Sort. `Where` duplicó esa lógica en vez de reutilizarla, posiblemente porque en `Where` el conjunto de filas supervivientes se construye desde un `setBits` iterator mientras `gatherRows` recibe `[]int`. Al añadir un nuevo tipo de columna (o cambiar la semántica de null en un gather) hay que editar dos sitios independientes.
- **Fix:** Generalizar `gatherRows` para aceptar un `iter.Seq[int]` en lugar de `[]int` (o añadir una variante `gatherSeq`), y reemplazar el switch de `Where` por un loop que llame `gatherRows(col, survivorSlice)`. Alternativamente, materializar los índices supervivientes en `[]int` al principio de `Where` (ya se tiene `n` por el popcount) y llamar a `gatherRows` directamente, que es la solución más simple.
- **Effort:** S
- **Confidence:** high

### [MEDIUM] `columnValidity` es un escape hatch que señala una ausencia en la interfaz `Column`

- **Location:** `/home/gverdugo/Projects/grizzly/groupby.go:319–329`
- **Category:** abstraction-leak
- **Description:** `columnValidity` hace un type-switch sobre los tres tipos concretos únicamente para exponer el campo `validity []uint64` que todos tienen pero que la interfaz `Column` no declara. El comentario lo llama "escape hatch for code that only needs validity". Aparece como parche para que `Count` en `Agg` pueda hacer el skip-null sin type-asserting al tipo completo.
- **Why it bites:** Cada vez que se añada un nuevo tipo de columna, `columnValidity` silenciosamente devuelve `nil` (el `default` del switch no existe — cae al `return nil` fuera), lo que haría que `Count` tratara todas las filas del nuevo tipo como válidas aunque hubiera nulls. El bug es silencioso y sólo se descubriría si el nuevo tipo tiene nulls y alguien escribe un test de `Count`.
- **Fix:** Añadir `Validity() []uint64` a la interfaz `Column` (o un método más idiomático `HasNulls() bool` + `NullBitmap() []uint64`). Entonces `columnValidity` desaparece y el código de `Count` llama `col.Validity()` directamente, con garantía estática de que cualquier implementación futura lo provee.
- **Effort:** S
- **Confidence:** high

### [LOW] `cmpBoolRows` duplica la lógica null-first de `cmpRows` en vez de componerla

- **Location:** `/home/gverdugo/Projects/grizzly/sort.go:101–126`
- **Category:** duplication
- **Description:** `cmpBoolRows` reimplementa manualmente el preámbulo null-first (líneas 109–118) que es byte-for-byte igual al de `cmpRows` (líneas 81–90). La única diferencia real es la extracción del valor bool a entero mediante `boolVal`.
- **Why it bites:** Si se cambia la semántica de "nulls first" (p. ej. para hacerla configurable), hay que recordar actualizar ambas funciones.
- **Fix:** Extraer el preámbulo null-first a una función `nullFirstOrder(vi, vj bool) (int, bool)` que devuelva `(resultado, decidido)`. Tanto `cmpRows` como `cmpBoolRows` la llaman; sólo si `!decidido` comparan valores. Cambio de 5 líneas.
- **Effort:** S
- **Confidence:** high

### [LOW] El buffer `256<<10` de los loaders es un magic number sin nombre

- **Location:** `/home/gverdugo/Projects/grizzly/from_csv.go:26`, `/home/gverdugo/Projects/grizzly/from_json.go:21`
- **Category:** magic-constants
- **Description:** `256<<10` (256 KB) aparece literalmente en dos sitios como argumento al buffer de lectura. No tiene nombre ni comentario que justifique ese tamaño concreto (vs. 64 KB o 1 MB). `maxPrintRows = 10` en `format.go` sí está nombrado.
- **Why it bites:** Bajo impacto en este tamaño de proyecto, pero si alguien quiere ajustar el buffer por benchmarks, tiene que encontrar y editar dos sitios y saber que son el mismo parámetro.
- **Fix:** Definir `const readerBufSize = 256 << 10 // 256 KB: cuts syscalls on large files` en un archivo compartido (o en cada loader con la misma constante local si se prefiere evitar un archivo nuevo). Cambio cosmético.
- **Effort:** S
- **Confidence:** medium

### [LOW] `floatValue` (singular) vs `floatValues` (plural) — vocabulario inconsistente en helpers de test

- **Location:** `/home/gverdugo/Projects/grizzly/loaders_test.go:19`, `/home/gverdugo/Projects/grizzly/groupby_test.go:37`
- **Category:** style-consistency
- **Description:** Los test helpers para leer columnas float tienen nombres distintos con semánticas distintas: `floatValue(t, df, col, i)` retorna un único `(float64, bool)` y `floatValues(t, df, name)` retorna todos los valores del tipo `([]float64, []bool)`. El plural/singular es el único indicio de la diferencia. `stringValues` (plural) existe en `groupby_test.go`; no hay `stringValue` singular.
- **Why it bites:** Bajo impacto ahora (5 archivos de test), pero confunde al leer tests nuevos: un desarrollador que ve `floatValue` tiene que ir a leer la firma para saber si devuelve uno o todos.
- **Fix:** Renombrar `floatValue` → `floatAt` o `floatValueAt` para que la distinción sea obvia por nombre, no por aridad. Cambio mecánico en un archivo.
- **Effort:** S
- **Confidence:** medium

### [INFO] `playground/main.go` usa una ruta relativa que depende del directorio de trabajo

- **Location:** `/home/gverdugo/Projects/grizzly/cmd/playground/main.go:62`
- **Description:** El playground asume que el proceso corre desde la raíz del repo (`go run ./cmd/playground`). Si se ejecuta desde otro directorio falla con un error de fichero no encontrado. Es aceptable para un playground de desarrollo, pero conviene documentarlo en el README o en un comentario.

### [INFO] `internal/logging/handler.go` — `WithGroup` implementado como no-op

- **Location:** `/home/gverdugo/Projects/grizzly/internal/logging/handler.go:76`
- **Description:** `WithGroup(string) slog.Handler { return h }` ignora silenciosamente los grupos de atributos. El comentario lo declara honestamente. **Nota post-auditoría:** este hallazgo quedó obsoleto durante la misma sesión — `internal/logging/` fue eliminado por completo en el commit `f930fba` (la librería usa solo `log/slog` de la stdlib).

---

## Prioritized Action Plan

Bucketed by urgency. Weighs severity, effort, and blast radius — not just raw severity.

### Do first (this sprint)

_CRITICAL items; HIGH items with S/M effort that block safe daily work._

- No hay items CRITICAL ni HIGH. La librería puede taggearse como v0.1.0 sin bloqueantes.

### Do next (next 30 days)

_HIGH with L effort + MEDIUM items that compound the longer they sit._

- [ ] [MEDIUM] `columnValidity` escape hatch → añadir accessor de validity a la interfaz `Column` — `groupby.go:319`, effort S
- [ ] [MEDIUM] `Where` duplica el gather por tipo → reutilizar `gatherRows` materializando los índices supervivientes — `mask.go:296`, effort S
- [ ] [MEDIUM] Parámetro `method string` en `compare` → derivar el nombre del `compareOp` — `mask.go:172`, effort S
- [ ] [MEDIUM] Patrón "collect-then-finish" duplicado en loaders → extraer `columnBuilder` (hacer ANTES de añadir un cuarto dtype o los writers To*) — `from_csv.go:72`, `from_json.go:55`, effort M

### Backlog / consider

_LOW / INFO / purely opportunistic fixes._

- [ ] [LOW] `cmpBoolRows` duplica el preámbulo null-first — `sort.go:101`, effort S
- [ ] [LOW] Nombrar la constante del buffer `256<<10` — `from_csv.go:26`, `from_json.go:21`, effort S
- [ ] [LOW] Renombrar `floatValue` → `floatAt` en tests — `loaders_test.go:19`, effort S
- [ ] [INFO] Documentar que el playground se ejecuta desde la raíz del repo — `cmd/playground/main.go:62`
- [x] [INFO] `WithGroup` no-op en el handler custom — resuelto: `internal/logging/` eliminado en `f930fba`

---

## Methodology & caveats

**Analyst:** one `clean-code-auditor` agent invocation (source of truth at `${CLAUDE_PLUGIN_ROOT}/agents/clean-code-auditor.md`). Read-only exploration (Read / Glob / Grep / Bash) — no target files were modified.

**Scope skipped:** `node_modules/`, `vendor/`, `dist/`, `build/`, `.next/`, `__pycache__/`, `.git/`, lockfiles, generated code, minified bundles, test fixtures with synthetic data.

**Sampling notes:**

**Cobertura del análisis:** Se leyeron los 23 archivos Go del repo (4 034 líneas en total), incluyendo todos los tests. La muestra fue completa (el repositorio es pequeño). Se excluyeron los archivos no-Go (`.md`, datos de test, `go.mod`).

**Hotspot analysis:** No se ejecutó `git log` para análisis de churn; con menos de 25 archivos y un proyecto en fase inicial, todos los archivos son igualmente recientes.

**Contexto del proyecto:** grizzly es un proyecto de aprendizaje. Los hallazgos se calibraron teniendo en cuenta que el objetivo es claridad educativa además de mantenibilidad. Las duplicaciones señaladas son reales pero no son bloqueantes; el código es deliberadamente explícito en muchos lugares para facilitar la lectura.

**Ausencias notables fuera de scope:** No hay documentación de API pública en formato README ni ejemplos ejecutables completos más allá del playground y los `Example*` functions en `example_test.go` — fuera de scope de este lens pero relevante para la madurez del proyecto.

**Severity legend:**

- `CRITICAL` — active maintenance blocker; the code cannot be safely changed without high risk. Fix now.
- `HIGH` — significant drag; this will keep biting. Fix this sprint.
- `MEDIUM` — real debt, contained. Schedule for next cleanup pass.
- `LOW` — minor or opportunistic. Fix when you're already in the file.
- `INFO` — observation only, not a defect.

**Out of scope for this audit:** correctness bugs, security vulnerabilities, performance bottlenecks, high-level module coupling and dependency direction. Use `dev:security-audit`, `dev:scalability-audit`, or `dev:architecture-audit` for those lenses.
