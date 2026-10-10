# Alcance del proyecto y estado de entregables

**Proyecto:** Sistema concurrente de predicción de riesgo de depresión posparto con K-means en Go
**Curso:** CC235 – Trabajo Final 2025-1
**Integrantes:** Mireya Nicole Sihuincha Schermuly · Maria Ximena Chavarria Barrios
**Repositorio:** https://github.com/sowiexsker894/Sistema-predicci-n-de-depresi-n-posparto-con-K-means-en-Go
**Última actualización:** 2026-10-10

---

## 1. Alcance del proyecto

### 1.1 Objetivo
Agrupar 1 000 000 de registros clínicos posparto en **3 niveles de riesgo (Bajo, Medio, Alto)**
mediante K-means, y comparar una versión **secuencial** con una **concurrente** (worker pool
con goroutines y channels) en Go, verificando formalmente la versión concurrente con Spin.

### 1.2 Flujo de datos

```
post natal data.csv (Kaggle, 1 503 filas)
  → primeravanceEDA.ipynb   → postpartum_depression_processed.parquet (1 503 × 11)
  → primeravanceData.ipynb  → postpartum_depression_1M.parquet + dataset_1m.csv (1 000 000 × 11)
  → main.go                 → niveles de riesgo, evaluación y tiempos
```

`dataset_1m.csv` (~200 MB) no se sube a GitHub (límite de 100 MB): está en `.gitignore`
y el README explica cómo regenerarlo.

### 1.3 Componentes

| Componente | Archivo | Qué hace |
|---|---|---|
| EDA y preprocesamiento | `primeravanceEDA.ipynb` | Codifica explícitamente cada respuesta (falla ante respuestas no previstas), imputa con la mediana, filtra la edad, normaliza con z-score y exporta a Parquet. |
| Aumento de datos | `primeravanceData.ipynb` | Genera 998 497 filas sintéticas con SDV, las convierte a los valores reales por cuantiles (mantiene las proporciones reales), deriva el target, valida el resultado y exporta Parquet y CSV. |
| K-means | `main.go` | Carga el CSV en memoria contigua, elige centroides con K-means++ (5 semillas, gana la de menor inercia), ejecuta la versión secuencial y la concurrente (worker pool), ordena los clusters por severidad (Bajo/Medio/Alto), evalúa contra el target y predice el riesgo de casos. |
| Tests y benchmarks | `main_test.go` | 13 tests (incluido secuencial = concurrente con el dataset real) y benchmarks por número de trabajadores y tamaño de bloque. |
| Verificación formal | `kmeans.pml`, `VERIFICACION_SPIN.md`, `spin_evidencia/` | Modelo Promela del worker pool, guía de ejecución y salidas de Spin. |

### 1.4 Decisiones de diseño
- **Target:** `Feeling sad or Tearful`. Sus dos columnas (normalizada y codificada) quedan
  **fuera** del clustering y solo se usan para evaluar; K-means usa **9 features**
  (Age + 8 síntomas).
- **Codificación:** No / Not at all = 0; Sometimes / Maybe / Often / Two or more days a week = 0.5;
  Yes = 1; "Not interested to say" se trata como dato faltante y se imputa con la mediana.
- **Niveles de riesgo:** los clusters se ordenan por la severidad media de sus síntomas,
  en la escala original 0 / 0.5 / 1.

---

## 2. Resultados principales

### 2.1 Clustering (semilla 3, 16 iteraciones, convergió)

| Síntoma (escala 0-1) | Bajo | Medio | Alto |
|---|---|---|---|
| Anxious | 0.02 | 0.99 | 0.83 |
| Guilt | 0.66 | 0.32 | 0.25 |
| Irritable | 0.41 | 0.50 | 0.67 |
| Suicide attempt | 0.16 | 0.00 | 1.00 |
| **Severidad media** | **0.34** | **0.44** | **0.55** |
| Registros | 303 463 | 440 614 | 255 923 |

**Pureza frente al target:** 0.386, con una línea base de 0.357. Los síntomas restantes
predicen poco la tristeza.

### 2.2 Rendimiento (mediana de 10 mediciones, mismo momento)

| Versión | Secuencial | 12 trabajadores | Speedup |
|---|---|---|---|
| G4 (worker pool) | 678 ms | 212 ms | 3.19x |
| G5 (+ memoria contigua) | 581 ms | 198 ms | 2.94x |

G5 mejora todos los tiempos; el speedup baja solo porque el secuencial mejora más.

### 2.3 Verificación formal (Spin 6.5.2)

| Caso | Resultado |
|---|---|
| Seguridad: deadlocks y exclusión mutua (9 314 estados) | ✅ `errors: 0` |
| Terminación: LTL `<> masterDone` con weak fairness | ✅ `errors: 0` |
| Modelo con error (el maestro no espera el último resultado) | ✅ Detectado: `assertion violated` |

### 2.4 Limitaciones para el informe y el video
- **Datos sintéticos:** las correlaciones son más débiles que las reales (|r| medio ≈ 0.09
  frente a 0.13) y el 97 % de las filas está repetido, porque las respuestas tienen solo
  2 o 3 opciones.
- **Clusters:** se separan sobre todo por dos síntomas binarios (intento de suicidio y
  ansiedad), una debilidad conocida de K-means con datos categóricos; K-modes queda como
  trabajo futuro. "Alto" agrupa a todos los registros con intento de suicidio, pero es el
  nivel con menos tristeza.
- **Niveles de riesgo:** son una etiqueta impuesta por el criterio de severidad, no algo
  que K-means descubra por sí solo.
- **Rendimiento:** el techo de unos 3x se explica por la CPU (i7-1265U: 2 núcleos rápidos
  y 8 de eficiencia), el ancho de banda de memoria compartido y las partes secuenciales
  (K-means++, reducción, actualización), según la ley de Amdahl. Los tiempos varían mucho
  entre sesiones en una laptop: solo se comparan mediciones hechas en la misma sesión.
- **Spin:** se verifica con 3 trabajadores, 4 trabajos y 2 iteraciones; no se modelan
  los cálculos.
- **Predicción:** los casos nuevos deben venir normalizados con la misma media y
  desviación del EDA.

---

## 3. Estado de los entregables

| # | Entregable | Estado | Dónde se hace |
|---|---|---|---|
| q | Verificación formal en Spin | ✅ Hecho: modelo, guía y evidencia | Repositorio + WSL |
| r | Informe de GAPs con IA (`.md`) | ✅ `docs/PROMPT_ANALISIS_IA.md` y `docs/INFORME_GAPS_IA.md` (Claude Opus 5.5, 22 GAPs) | Repositorio |
| s | Conclusiones y recomendaciones | ✅En la sección 10 de `CC65-TP-202620_TP.docx` | Informe |
| t | Referencias APA | ✅ Sección 11 del informe (31 referencias, APA 7) | Informe |
| u | Código en GitHub con historial | ✅ Subido a `main` | Git + web de GitHub |
| v | Video de hasta 6 minutos | ✅ Subido a carpeta Drive| PPT y Canva |

### Mediciones de rendimiento
Para el informe se usan las **mediciones A/B intercaladas** de la sección 2.2: cada
versión se midió alternada con la anterior en la misma sesión, así que la comparación es
justa aunque los tiempos absolutos varíen entre sesiones. No se hizo una medición final
controlada; `medicion_final.ps1` permite repetirla (tests, benchmarks y tabla en
`resultados/`) con la laptop conectada, aplicaciones cerradas y modo "Máximo rendimiento".

### Pendientes técnicos
- **Notebooks:** recomendaciones de prioridad media y baja de la Fase 2 (M3-M7, B1-B7).
- Subir los entregables restantes (r, s, t, enlace del video) **antes** de la fecha de
  entrega: un commit posterior al plazo resta 10 puntos.

---

## 4. Checklist antes de la entrega

- [x] Datos: codificación del EDA corregida y datos sintéticos validados
- [x] K-means sin el target, K-means++ y niveles de riesgo
- [x] Worker pool y memoria contigua, con tests y benchmarks
- [x] Verificación en Spin con evidencia (q)
- [x] Mediciones de rendimiento (A/B intercaladas; script `medicion_final.ps1` para repetirlas)
- [x] Informe de GAPs con IA (r)
- [x] Informe Word actualizado: `CC65-TP-202620_TP.docx` (original de la PC2: `CC65-PC2-202620.docx`)
- [x] Verificar las referencias de la PC1 (t)
- [x] Push a GitHub (u)
- [x] Confirmar con el docente si `main` es aceptable en lugar de `master` (u)
- [x] Video de hasta 6 minutos con ambas integrantes, publicado, con la URL en el informe (v)
