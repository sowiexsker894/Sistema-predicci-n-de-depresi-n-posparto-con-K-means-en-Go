# Informe de GAPs: análisis del código con IA

| | |
|---|---|
| **Repositorio** | https://github.com/sowiexsker894/Sistema-predicci-n-de-depresi-n-posparto-con-K-means-en-Go |
| **Versión analizada** | rama `main`, commit `cf31459` |
| **Modelo de IA** | Claude Opus 5.5 (Anthropic), mediante Claude Code |
| **Prompt** | [`PROMPT_ANALISIS_IA.md`](PROMPT_ANALISIS_IA.md) |
| **Fecha** | 2026-10-10 |

---

## 1. Resumen ejecutivo

El sistema está en buen estado para un trabajo de curso. La versión concurrente es
**correcta**: los tests demuestran que da los mismos centroides que la secuencial, y Spin
verificó el protocolo del worker pool, incluido un caso con error que sí se detecta. El
código compila sin advertencias de `go vet`, cumple `gofmt` y usa solo la biblioteca estándar.

Los tres GAPs más importantes son:

1. **El detector de data races de Go nunca se ejecutó** (CONC-01). Es la herramienta
   estándar para validar código concurrente en Go, y no se pudo usar en Windows porque
   requiere `cgo` y `gcc`.
2. **K-means no es el algoritmo más adecuado para estos datos** (DATA-01). Todas las
   variables son categóricas de 2 o 3 valores, los clusters quedan dominados por dos
   síntomas binarios y la pureza frente al target es 0.386, apenas por encima de la línea
   base de 0.357.
3. **Una parte importante del trabajo es secuencial** (CONC-02). K-means++ recorre el
   millón de puntos en un solo hilo antes de que empiecen los trabajadores. Medido: es
   ~25 % del tiempo de la versión concurrente con 12 trabajadores, lo que limita el
   speedup según la ley de Amdahl.

---

## 2. Metodología

Se clonó el repositorio en el commit `cf31459` y se revisaron `main.go` (512 líneas),
`main_test.go`, `kmeans.pml`, los dos notebooks, `.gitignore` y la documentación.

| Herramienta | Resultado |
|---|---|
| `gofmt -l .` | Sin archivos por formatear |
| `go vet ./...` | Sin advertencias |
| `go test -count=1 .` | 13 tests: todos pasan (incluye el dataset real de 1M) |
| `go test -short -cover .` | Cobertura del 69.3 % de las sentencias |
| `go test -race` | **No ejecutable:** `CGO_ENABLED=0` y no hay `gcc` en el equipo Windows |
| Medición de `initialCentroids` | ~55 ms por ejecución (5 repeticiones sobre el dataset real) |
| Spin 6.5.2 (`spin_evidencia/`) | Seguridad y terminación: `errors: 0`. Modelo con error: detectado |

Los hallazgos marcados con 🔬 se comprobaron ejecutando herramientas; el resto proviene de
la lectura del código, con su ubicación para que se pueda verificar.

---

## 3. Fortalezas

- **Corrección demostrada:** `TestConcurrentMatchesSequential` y
  `TestConcurrentMatchesSequentialRealData` comparan la versión concurrente con la
  secuencial con 1 a 20 trabajadores, y con el millón de registros real.
- **Sincronización sin mutex:** cada trabajo lleva los centroides de su iteración, que
  nadie modifica (`Job.Centroids`), y el maestro los reemplaza solo después de recibir
  todos los resultados (`main.go:383`). Así se evitan los data races por diseño.
- **Worker pool bien cerrado:** `close(jobs)` y `wg.Wait()` (`main.go:405`) garantizan
  que no queden goroutines vivas al terminar.
- **Verificación formal con un caso negativo:** el modelo `-DERROR_SIN_ESPERA` demuestra
  que las propiedades verificadas detectan errores reales.
- **Medición rigurosa:** comparaciones A/B intercaladas con mediana de 10 mediciones.
- **Manejo de errores en la carga:** distingue `io.EOF` de los errores de lectura y cuenta
  las filas descartadas.

---

## 4. GAPs identificados

**Severidad:** **Alta** compromete la validez de los resultados o de la verificación.
**Media** es un riesgo real que conviene corregir. **Baja** es una mejora de calidad.

### 4.1 Patrones de concurrencia

| ID | Sev. | Ubicación | Descripción | Impacto | Recomendación |
|---|---|---|---|---|---|
| CONC-01 🔬 | **Alta** | `main_test.go` (ejecución) | El detector de data races (`go test -race`) nunca se ejecutó: requiere `cgo` y no hay `gcc` en Windows (`CGO_ENABLED=0`). | La ausencia de races se apoya en el diseño y en Spin, pero no en la herramienta estándar de Go sobre el código real. | Ejecutar `go test -race ./...` en WSL o Linux, donde ya hay `gcc`, y agregarlo a la integración continua (QA-02). |
| CONC-02 🔬 | **Alta** | `main.go:170` (`initialCentroids`) | K-means++ recorre el millón de puntos en un solo hilo, dentro del tiempo medido de `KMeansConcurrent`. Medido (5 ejecuciones): **~55 ms**, que es el **~5.7 %** del tiempo secuencial pero el **~25 %** del tiempo concurrente con 12 trabajadores. | Con una fracción secuencial de ~5.7 %, la ley de Amdahl limita el speedup a ~17x aunque haya núcleos de sobra; en la práctica, una cuarta parte del tiempo concurrente no se paraleliza. Explica parte del techo de ~3x observado. | Paralelizar con los mismos trabajadores el cálculo de `minDist` de K-means++, o medirlo aparte para reportar el speedup solo del bucle de iteraciones. |
| CONC-03 | Media | `main.go:383` (reducción) | Los resultados llegan en el orden en que terminan los trabajadores y se suman en ese orden. Como la suma de `float64` no es asociativa, dos ejecuciones pueden diferir en los últimos decimales (~1e-12). | Resultados no deterministas bit a bit; en un caso límite podría cambiar el número de iteraciones. Los tests lo toleran con 1e-9. | Guardar cada resultado parcial en la posición de su trabajo (índice en `Job`) y sumarlos siempre en el mismo orden. |
| CONC-04 | Baja | `main.go:383` | La reducción la hace solo el maestro, de forma secuencial (100 resultados × K × 9 por iteración). | Pequeña fracción secuencial; hoy es despreciable frente al cálculo de distancias. | Mantener; documentarla como parte secuencial en el análisis de Amdahl. |
| CONC-05 | Baja | `main.go:546` | Las 5 semillas de K-means++ se prueban una tras otra, aunque cada ejecución es independiente. | Tiempo total de `main` mayor del necesario (no afecta a las mediciones de los benchmarks). | Opcional: ejecutar las semillas en goroutines distintas y quedarse con la de menor inercia. |
| CONC-06 | Baja | `main.go:331` (`worker`) | Si un trabajador entra en `panic`, el programa completo termina sin un mensaje claro sobre qué trabajo falló. | Diagnóstico difícil ante datos corruptos. | Para un sistema en producción: `recover` en el trabajador y enviar el error por un canal. |

### 4.2 Calidad de código

| ID | Sev. | Ubicación | Descripción | Impacto | Recomendación |
|---|---|---|---|---|---|
| QC-01 | Media | `main.go:315`, `main_test.go:273, 363` | `ChunkSize` es una variable global que los tests modifican para probar otros tamaños. | Si en el futuro se ejecutan tests en paralelo (`t.Parallel()`), habría un data race sobre `ChunkSize`. | Pasar el tamaño de bloque como parámetro de `KMeansConcurrent` o en un struct de configuración. |
| QC-02 | Baja | `main.go` (512 líneas) | Todo el sistema (carga, K-means, evaluación, presentación) está en un solo archivo del paquete `main`, con nombres exportados (`KMeansSequential`, `Job`) que no se pueden usar desde otro paquete. | Dificulta reutilizar K-means, por ejemplo desde una API. | Separar en paquetes (`dataset`, `kmeans`, `riesgo`) y dejar en `main` solo la presentación. |
| QC-03 | Baja | `main.go:88` | La capacidad inicial `1000000*Dimensions` está fija en el código. | Con otro tamaño de dataset, la memoria se ajusta con `append` (funciona, pero el valor no es un parámetro). | Definir una constante con nombre o estimar la capacidad a partir del tamaño del archivo. |
| QC-04 🔬 | Baja | `printRiskLevels`, `inertia`, `main` | La cobertura de tests es de 69.3 %; las funciones de presentación e `inertia` tienen 0 %. | Un cambio en el cálculo de la pureza o de la inercia no lo detectaría ningún test. | Separar los cálculos (pureza, inercia) de la impresión y probarlos con datos conocidos. |

### 4.3 Seguridad, privacidad y robustez

| ID | Sev. | Ubicación | Descripción | Impacto | Recomendación |
|---|---|---|---|---|---|
| SEC-01 🔬 | Media | `.gitignore` | El notebook EDA pide subir `kaggle.json` (credenciales de la API de Kaggle), pero `.gitignore` no lo excluye. | Riesgo de publicar las credenciales en el repositorio público. | Agregar `kaggle.json` a `.gitignore`. |
| SEC-02 | Media | `main.go:465` (`predictRisk`) | La predicción espera un caso **normalizado** con la media y la desviación del EDA, pero esos valores no se guardan ni se exponen. | Un caso nuevo con valores sin normalizar (0/0.5/1) recibiría un nivel de riesgo incorrecto, sin aviso. | Guardar los parámetros de normalización en un archivo (por ejemplo JSON) junto al Parquet y aplicarlos dentro de `predictRisk`. |
| SEC-03 | Media | Uso del sistema | Los datos incluyen información clínica sensible (intento de suicidio) y la salida asigna un "riesgo". | Riesgo de que el resultado se interprete como diagnóstico. | Declarar en el README y en la salida que es una herramienta académica de apoyo, no un diagnóstico, y que los datos de 1M son sintéticos. |
| SEC-04 | Baja | `primeravanceData.ipynb` (celda 0), `primeravanceEDA.ipynb` (celda 2) | `pip install` sin versiones fijas. | Una versión nueva de SDV o Polars puede cambiar los resultados o romper el notebook. | Fijar versiones: `sdv==1.38.3 polars==1.35.2`. |
| SEC-05 | Baja | `main.go:78` | La carga lee el archivo completo en memoria, sin límite de tamaño. | Con un archivo mucho mayor que la RAM, el programa termina por falta de memoria. | Aceptable para 1M de registros; para más datos, leer en bloques o validar el tamaño antes de cargar. |

### 4.4 Verificación y pruebas

| ID | Sev. | Ubicación | Descripción | Impacto | Recomendación |
|---|---|---|---|---|---|
| QA-01 | Media | `kmeans.pml` | El modelo de Spin es una abstracción mantenida a mano: usa mensajes `STOP` en lugar de `close`, valores pequeños (3 trabajadores, 4 trabajos, 2 iteraciones) y no modela los datos. | Si el código Go cambia y el modelo no, la verificación deja de representar al programa. | Mantener la tabla de correspondencia de `VERIFICACION_SPIN.md` y revisar el modelo en cada cambio de la parte concurrente. |
| QA-02 🔬 | Media | Repositorio (no existe `.github/`) | No hay integración continua. | Los tests, `go vet` y el detector de races dependen de que alguien los ejecute a mano. | Un workflow de GitHub Actions con `go vet`, `go test -short` y `go test -race -short` en cada push. |
| QA-03 | Baja | Benchmarks | Las mediciones se hicieron en una laptop con CPU híbrida y variación alta entre sesiones. | Los tiempos absolutos no son comparables entre sesiones ni con los de otro equipo. | Repetir con `medicion_final.ps1` en condiciones controladas y en más de un equipo. |

### 4.5 Datos y modelo

| ID | Sev. | Ubicación | Descripción | Impacto | Recomendación |
|---|---|---|---|---|---|
| DATA-01 | **Alta** | Diseño (K-means con datos categóricos) | Las 9 variables son categóricas de 2 o 3 valores. K-means usa distancias euclidianas, y los clusters quedan dominados por dos síntomas binarios (intento de suicidio y ansiedad). Pureza 0.386 frente a 0.357 de línea base. | Los niveles Bajo/Medio/Alto son poco informativos clínicamente. | Comparar con K-modes (diseñado para datos categóricos) y justificar K = 3 con el método del codo o silhouette. |
| DATA-02 | Media | Evaluación | Solo se mide la pureza frente a un target. No hay silhouette ni análisis de estabilidad entre semillas. | No se sabe qué tan separados ni qué tan estables son los clusters. | Calcular silhouette sobre una muestra y comparar las asignaciones entre semillas. |
| DATA-03 | Media | `primeravanceData.ipynb` (celdas 2-3) | El muestreo de SDV no usa semilla fija. | Cada ejecución genera un millón de filas distinto; los resultados no son reproducibles exactamente. | Fijar la semilla antes de entrenar y generar, y verificar que dos ejecuciones den el mismo archivo. |
| DATA-04 | Baja | Datos sintéticos | Las correlaciones sintéticas son más débiles que las reales (\|r\| medio ≈ 0.09 frente a 0.13) y el 97 % de las filas está repetido. | El millón de registros sirve para medir rendimiento, pero no agrega información clínica nueva. | Declararlo como limitación; usar los 1 503 registros reales para las conclusiones clínicas. |

---

## 5. Plan de acción priorizado

| Prioridad | Acción | GAPs | Esfuerzo |
|---|---|---|---|
| 1 | Ejecutar `go test -race ./...` en WSL y documentar el resultado | CONC-01 | Bajo |
| 2 | Agregar `kaggle.json` a `.gitignore` y la advertencia de uso no diagnóstico | SEC-01, SEC-03 | Bajo |
| 3 | GitHub Actions con `go vet`, tests y detector de races | QA-02, CONC-01 | Bajo |
| 4 | Guardar los parámetros de normalización y usarlos en `predictRisk` | SEC-02 | Medio |
| 5 | Paralelizar K-means++ o medir aparte su tiempo (~25 % del tiempo concurrente) | CONC-02 | Medio |
| 6 | Sumar los resultados parciales en orden fijo | CONC-03 | Bajo |
| 7 | `ChunkSize` como parámetro en lugar de variable global | QC-01 | Bajo |
| 8 | Comparar con K-modes y agregar silhouette | DATA-01, DATA-02 | Alto |
| 9 | Fijar semilla y versiones en los notebooks | DATA-03, SEC-04 | Bajo |

---

## 6. Limitaciones del análisis

- El detector de races no se pudo ejecutar en el equipo usado (ver CONC-01), así que la
  ausencia de data races se apoya en el diseño, en los tests y en Spin.
- El tiempo de K-means++ (CONC-02) se midió con `time.Since` en 5 ejecuciones sobre el
  dataset real, no con un profiler; los porcentajes varían con la carga del equipo.
- El análisis lo hizo un modelo de IA. Cada hallazgo indica su ubicación para que los
  autores lo puedan verificar, y las decisiones finales corresponden a los autores.
