# Verificación formal con Spin

El modelo `kmeans.pml` representa el **worker pool** de `KMeansConcurrent` en `main.go`:
un maestro que reparte trabajos por un canal, trabajadores permanentes que los procesan
y el cierre final (`close(jobs)` + `wg.Wait()`).

## Correspondencia entre el modelo y el código Go

| Promela (`kmeans.pml`) | Go (`main.go`) |
|---|---|
| `chan jobs = [NUM_JOBS] of {...}` | `jobs := make(chan Job, numJobs)` |
| `chan results = [NUM_JOBS] of {...}` | `results := make(chan PartialResult, numJobs)` |
| `proctype Worker` | `func worker(...)` |
| `readers++ ... readers--` | lectura de los centroides en `assignAndSum` |
| Maestro: repartir / reducción / actualizar | Pasos 1, 2 y 3 del bucle de `KMeansConcurrent` |
| `centroidsVersion++` | `centroids = newCentroids` |
| Opción `break` "convergió" | `if maxShift < Threshold { break }` |
| `jobs ! STOP` (uno por trabajador) | `close(jobs)` |
| `finished++` / `(finished == NUM_WORKERS)` | `wg.Done()` / `wg.Wait()` |

Promela no tiene `close` para canales: se modela con un mensaje `STOP` por trabajador,
enviado después de todos los trabajos (el canal es FIFO, así que ningún trabajador ve
el `STOP` antes de terminar los trabajos pendientes).

## Propiedades verificadas

1. **Ausencia de deadlocks:** ningún proceso queda bloqueado para siempre; todos
   terminan (Spin lo reporta como *invalid end states*).
2. **Exclusión mutua sobre los centroides:**
   - `assert(readers == 0)` al actualizar: nunca se modifican mientras un trabajador
     los lee.
   - `assert(!updating)` al empezar a leer: ningún trabajador lee durante la actualización.
   - `assert(jobIter == centroidsVersion)`: cada trabajador lee los centroides de la
     iteración de su trabajo.
   - `assert(resIter == iter)`: el maestro no mezcla resultados de distintas iteraciones.
3. **Terminación (LTL):** `ltl termina { <> masterDone }`, es decir, en toda ejecución
   el maestro llega al final.

## Cómo ejecutarlo

### Opción A: línea de comandos (Linux, WSL o Windows con Spin y gcc)

**1. Seguridad: deadlocks y asserts** (`-DSAFETY` activa el modo de seguridad y `-DNOCLAIM` deja fuera la fórmula LTL)

```bash
spin -a kmeans.pml
gcc -DSAFETY -DNOCLAIM -o pan pan.c
./pan
```

Resultado esperado (los números de estados y algunos textos pueden variar según la versión de Spin):

```
Full statespace search for:
	never claim         	- (not selected)
	assertion violations	+
	cycle checks       	- (disabled by -DSAFETY)
	invalid end states	+

State-vector ... byte, depth reached ..., errors: 0
```

Lo importante: `assertion violations +`, `invalid end states +` y **`errors: 0`**.

**2. Terminación: fórmula LTL**

```bash
spin -a kmeans.pml
gcc -o pan pan.c
./pan -a -f
```

Resultado esperado: `never claim + (termina)`, `acceptance cycles + (fairness enabled)`
y **`errors: 0`**.

**3. Comprobar que Spin detecta errores: modelo con un error a propósito**

El flag `-DERROR_SIN_ESPERA` hace que el maestro actualice los centroides **sin esperar
el último resultado**:

```bash
spin -DERROR_SIN_ESPERA -a kmeans.pml
gcc -DSAFETY -DNOCLAIM -o pan pan.c
./pan
```

Resultado esperado: **`assertion violated`** y **`errors: 1`**. Para ver la ejecución
que lleva al error:

```bash
spin -DERROR_SIN_ESPERA -t -p kmeans.pml
```

### Opción B: iSpin / jSpin (interfaz gráfica)

1. Abrir `kmeans.pml`.
2. **Seguridad:** en la pestaña *Verification* elegir *Safety*, marcar *assertion
   violations* e *invalid endstates*, y ejecutar (*Run*). Debe terminar con `errors: 0`.
3. **Terminación:** elegir *Liveness* / *use claim* con la fórmula `termina` y
   *weak fairness*, y ejecutar. Debe terminar con `errors: 0`.
4. **Modelo con error:** agregar `-DERROR_SIN_ESPERA` en las opciones del preprocesador
   (o escribir `#define ERROR_SIN_ESPERA` en la primera línea del modelo, solo para
   esta prueba) y repetir el paso 2. Debe reportar `assertion violated`.

## Resultados obtenidos

Ejecutado con Spin 6.5.2 en WSL (Ubuntu). Las salidas completas están en `spin_evidencia/`.

| Caso | Archivo | Resultado |
|---|---|---|
| 1. Seguridad (deadlocks y asserts) | `caso1_seguridad.txt` | ✅ `errors: 0`: 9 314 estados, profundidad 154 |
| 2. Terminación (LTL `termina`, weak fairness) | `caso2_terminacion.txt` | ✅ `errors: 0`: 9 304 estados (88 191 visitados) |
| 3. Modelo con error (`-DERROR_SIN_ESPERA`) | `caso3_error.txt`, `caso3_traza.txt` | ✅ Error detectado: `assertion violated (jobIter==centroidsVersion)` |

En los casos 1 y 2, Spin reporta `unreached ... (0 of N states)` para `Worker`, `Master`
e `init`: todas las instrucciones del modelo se alcanzaron, incluidas la convergencia
temprana y la convergencia en la última iteración.

### Lectura de la traza del caso 3

1. Iteración 0: el maestro pone 4 trabajos en la cola, pero por el error solo espera
   3 resultados.
2. Paso 58: el maestro actualiza los centroides (`centroidsVersion` pasa de 0 a 1)
   aunque el 4.º trabajo de la iteración 0 sigue pendiente en la cola.
3. El maestro empieza la iteración 1 y pone 4 trabajos nuevos.
4. Paso 72: un trabajador toma el trabajo viejo de la iteración 0.
5. Pasos 79-80: ese trabajador lee los centroides, que ya son los de la iteración 1,
   y falla el assert `jobIter == centroidsVersion`.

En el programa real, esos puntos se clasificarían con centroides de otra iteración y
el resultado sería distinto al secuencial. El código Go evita este error porque el
maestro espera **un resultado por cada trabajo** (`for j := 0; j < numJobs; j++ { res := <-results ... }`)
antes de actualizar los centroides. El caso 3 muestra que la verificación sí detecta
errores de sincronización, así que los `errors: 0` de los casos 1 y 2 tienen valor.

## Limitaciones del modelo

- Se verifica con valores pequeños (3 trabajadores, 4 trabajos por iteración y
  2 iteraciones) para que el espacio de estados sea manejable. El protocolo es el
  mismo para cualquier tamaño, pero Spin solo garantiza el resultado para estos valores.
- No se modelan los datos ni los cálculos (sumas, distancias), solo la sincronización.
- El `close(jobs)` de Go se representa con mensajes `STOP`, porque Promela no tiene
  cierre de canales.
