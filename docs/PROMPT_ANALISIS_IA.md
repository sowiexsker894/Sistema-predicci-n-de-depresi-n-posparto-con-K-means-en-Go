# Prompt estructurado para el análisis del código con IA

## 1. Modelo de IA seleccionado

**Modelo:** Claude Opus 5.5 (Anthropic), usado mediante **Claude Code**, el agente de
programación que lee y ejecuta comandos sobre el repositorio.

**Justificación de la elección:**

| Criterio | Por qué es importante en este proyecto |
|---|---|
| Acceso al repositorio completo | Claude Code lee todos los archivos (Go, Promela, notebooks, configuración) y no solo fragmentos copiados en un chat, así que el análisis puede relacionar el código Go con el modelo de Spin y con los notebooks. |
| Puede ejecutar herramientas | Comprueba sus afirmaciones con `go vet`, `go test` y búsquedas en el código, en lugar de suponer. |
| Razonamiento sobre concurrencia | Puede analizar intercalados de goroutines, canales y sincronización, que es el foco del curso. |
| Ventana de contexto amplia | El código, los tests, el modelo Promela y la documentación caben en un solo análisis. |

**Código analizado:** repositorio público
https://github.com/sowiexsker894/Sistema-predicci-n-de-depresi-n-posparto-con-K-means-en-Go,
rama `main`, commit `cf31459`.

**Resultado:** [`INFORME_GAPS_IA.md`](INFORME_GAPS_IA.md).

## 2. Prompt utilizado

El prompt sigue una estructura de **rol, contexto, tarea, criterios, restricciones y formato
de salida**:

```text
# ROL
Actúa como un ingeniero de software senior especializado en programación concurrente en
Go, verificación formal y calidad de software, que revisa el trabajo final de un curso
universitario de Programación Concurrente y Distribuida.

# CONTEXTO
El repositorio implementa un sistema que agrupa 1 000 000 de registros clínicos de
depresión posparto en 3 niveles de riesgo (Bajo, Medio, Alto) con K-means:
- primeravanceEDA.ipynb y primeravanceData.ipynb (Python, Google Colab): limpieza,
  normalización y aumento de datos con SDV hasta 1M de filas.
- main.go: carga del CSV, K-means secuencial y concurrente (worker pool con canales y
  sync.WaitGroup), K-means++, niveles de riesgo y predicción. Solo biblioteca estándar.
- main_test.go: tests y benchmarks.
- kmeans.pml: modelo Promela del worker pool verificado con Spin.
Los autores son estudiantes con nivel intermedio de Go.

# TAREA
Analiza el código del repositorio (rama main, commit cf31459) e identifica los GAPs:
las diferencias entre el estado actual y lo esperable en un sistema concurrente de
calidad profesional.

# CRITERIOS DE ANÁLISIS
1. Calidad de código: legibilidad, organización, duplicación, nombres, estado global,
   manejo de errores, mantenibilidad.
2. Seguridad y robustez: validación de entradas, consumo de recursos, manejo de
   credenciales, datos sensibles de salud, dependencias.
3. Patrones de concurrencia: corrección (data races, deadlocks, fugas de goroutines),
   sincronización, determinismo, escalabilidad y partes secuenciales (ley de Amdahl).
4. Verificación y pruebas: cobertura de tests, detector de carreras, integración
   continua, alcance del modelo de Spin respecto al código.
5. Datos y modelo: validez del uso de K-means con estos datos, evaluación,
   reproducibilidad.

# RESTRICCIONES
- Cada hallazgo debe ser verificable: indica el archivo y la línea, o el comando que lo
  demuestra. No inventes problemas que no se puedan comprobar en el código.
- Distingue entre lo que comprobaste ejecutando herramientas y lo que deduces leyendo.
- Reconoce también las fortalezas del código, no solo los problemas.
- Las recomendaciones deben ser alcanzables para estudiantes de nivel intermedio.

# FORMATO DE SALIDA
Informe en Markdown con:
1. Resumen ejecutivo (estado general y los 3 GAPs más importantes).
2. Metodología (qué se revisó y qué herramientas se ejecutaron).
3. Fortalezas encontradas.
4. Tabla de GAPs por categoría: ID, severidad (Alta / Media / Baja), ubicación,
   descripción, impacto y recomendación.
5. Plan de acción priorizado.
6. Limitaciones del análisis.
```
