# Sistema concurrente de predicción de depresión posparto con K-means en Go

## 📋 Descripción General

Desarrollar un sistema concurrente de predicción de riesgo de depresión posparto basado en clustering con K-means implementado en Go, que permita procesar grandes volúmenes de datos clínicos.

---

## 👥 Integrantes del Proyecto

*Mireya Nicole Sihuincha Schermuly*
*Ma Ximena Chavarria Barrios*

---

## 🎯 Objetivo Principal

Procesar el dataset PPD (Postpartum Depression) con máxima eficiencia, implementando:
- Ingesta de datos desde Kaggle
- Transformaciones lazy con Polars
- Normalización de características
- Validación de calidad
- Exportación en formato Parquet

---

## 📊 Análisis Exploratorio de Datos (EDA)

### Dependencias Utilizadas

```python
polars          # Procesamiento paralelo lazy
pyarrow         # Soporte para Parquet
kaggle          # Descarga de datasets
dask            # Procesamiento distribuido
requests        # HTTP requests
psutil          # Monitoreo de recursos
```

### Flujo de Procesamiento

#### 1️⃣ **Configuración de Kaggle API**
- Descarga del archivo `kaggle.json` desde https://www.kaggle.com/settings/account
- Configuración del directorio `.kaggle` con permisos seguros (0o600)
- Descarga automática del dataset desde Kaggle

#### 2️⃣ **Carga con Polars (Lazy Evaluation)**

Polars utiliza **lazy evaluation** para optimizar el consumo de memoria:

```python
df_lazy = pl.scan_csv(csv_path)  # NO carga datos en memoria
# Solo crea un plan de ejecución que se optimiza automáticamente
```

**Ventajas:**
- ✓ Procesamiento sin cargar todo en RAM
- ✓ Plan de ejecución optimizado
- ✓ Procesamiento paralelo automático

**Estadísticas del Dataset:**
- Archivo: `post natal data.csv`
- Tamaño: ~0.1 MB
- Registros: 1,503
- Columnas: 11 (1 timestamp + 10 variables clínicas)

#### 3️⃣ **Autodetección de Estructura**

El sistema detecta automáticamente:

**Columnas numéricas:** 0
**Columnas categóricas:** 11
- `Timestamp` - Fecha del registro
- `Age` - Rango de edad (18-20, 20-25, ..., 50-55)
- `Feeling sad or Tearful` - Variable TARGET (3 valores: Yes, No, Sometimes)
- `Irritable towards baby & partner` - Síntoma
- `Trouble sleeping at night` - Síntoma
- `Problems concentrating or making decision` - Síntoma
- `Overeating or loss of appetite` - Síntoma
- `Feeling anxious` - Síntoma
- `Feeling of guilt` - Síntoma
- `Problems of bonding with baby` - Síntoma
- `Suicide attempt` - Síntoma crítico

#### 4️⃣ **Transformaciones Lazy (Sin ejecutar aún)**

##### a) Conversión de Tipos: STRING → NÚMERO

```python
# Age: Convertir rango a valor central
'18-20'  → 19
'20-25'  → 22.5
'25-30'  → 27.5
'30-35'  → 32.5
'35-40'  → 37.5
'40-45'  → 42.5
'45-50'  → 47.5
'50-55'  → 52.5

# Respuestas binarias/ternarias
'Yes'                 → 1
'No'                  → 0
'Sometimes'           → 0.5
'Maybe'               → 0.5
'Not interested to say' → 0 (para Suicide attempt)
```

##### b) Imputación: Llenar valores faltantes

```python
# Usar mediana de cada columna para valores NULL
Age_numeric = Age_numeric.fillna(median)
Síntomas_encoded = Síntomas_encoded.fillna(median)
```

##### c) Normalización: Z-score (media=0, std=1)

**Crítico para K-means:** Normalizar escala diferente
- Age: 18-50 años
- Síntomas: 0-1 escala binaria

```python
normalized = (valor - media) / desviación_estándar
```

**Resultado:** Todas las características con media ≈ 0 y std ≈ 1

##### d) Validación: Filtrar registros inválidos

```python
# Filtros aplicados:
# - Age entre 18-50 años
# - Sin valores extremos (outliers)
```

#### 5️⃣ **Selección de Columnas Finales**

- **Features seleccionadas:** 11 (solo normalizadas)
- **Target:** `Feeling sad or Tearful_encoded`
- **Formato:** Ready para clustering con K-means

#### 6️⃣ **Ejecución del Plan Optimizado (.collect())**

```
Ejecución completada
Tiempo: 0.01 segundos
RAM pico: 0.9 MB
Registros procesados: 1,503
Features: 11
```

**Validación de Calidad:**
- ✓ Registros originales: 1,503
- ✓ Registros después de limpieza: 1,503
- ✓ Registros eliminados (inválidos): 0
- ✓ Valores faltantes: 0
- ✓ Duplicados encontrados: 1,185

**Estadísticas de Features Normalizados:**
```
                    Media    StdDev    Min      Max
Age_normalized      0.955    1.0     -1.663   1.449
Feeling sad_norm    0.180    1.0     -1.200   1.181
Irritable_norm     -0.002    1.0     -1.231   1.152
...
```

#### 7️⃣ **Guardar en Parquet**

```
Ubicación: data/postpartum_depression_processed.parquet
Tamaño CSV:     0.11 MB
Tamaño Parquet: 0.02 MB
Compresión:     4.4x más pequeño
Tiempo escritura: 0.03s
```

**Ventajas del formato Parquet:**
- ✓ 100x más rápido que CSV
- ✓ Compresión automática (snappy)
- ✓ Soporte para lazy loading
- ✓ Compatible con Arrow/Polars/Dask

---

## 🚀 Próximos Pasos

1. **Implementación del Clustering K-means en Go**
   - Cargar datos desde Parquet
   - Ejecutar K-means con optimizaciones concurrentes
   - Evaluación de clusters (silhouette, inertia)

2. **Integración de Goroutines**
   - Procesamiento paralelo de datos
   - Comunicación vía channels
   - Sincronización eficiente

3. **API REST para predicciones**
   - Endpoint para nuevos casos
   - Retorno de cluster asignado + riesgo
   - Documentación Swagger

---

## 📁 Estructura del Proyecto

```
.
├── README.md
├── primeravanceEDA (1).ipynb        # Análisis y preprocesamiento
├── data/
│   ├── post natal data.csv
│   └── postpartum_depression_processed.parquet
└── [código Go próximamente]
```

---

## 📝 Notas Técnicas

- **Lazy Evaluation:** Polars optimiza automáticamente el pipeline de transformaciones
- **Z-score Normalization:** Esencial para K-means, evita sesgo por escala
- **Parquet Format:** Mejor opción para ML pipelines, soporte nativo en Apache Spark/Dask
- **Duplicados:** El dataset tiene 1,185 duplicados (78.9%), considerar para data augmentation

---

## ⚖️ Licencia

[Especificar licencia]

---

**Última actualización:** 2026-09-13
