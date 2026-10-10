package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	K           = 3    // 3 niveles de riesgo clínico (Bajo, Medio, Alto)
	Dimensions  = 9    // 9 características: Age + 8 síntomas (sin 'Feeling sad', que es el target)
	Threshold   = 1e-4 // Tolerancia de convergencia (se compara con la distancia al cuadrado)
	MaxIters    = 100  // Máximo de iteraciones
	Restarts    = 5    // Inicializaciones K-means++ que se prueban en main (gana la de menor inercia)
	DefaultSeed = 3    // Semilla de tests y benchmarks: la que main elige como mejor (menor inercia)
)

// Columnas del CSV:
//
//	0     Age_normalized                      -> feature
//	1     Feeling sad or Tearful_normalized   -> NO se usa: es el mismo dato que el target
//	2-9   síntomas normalizados               -> features
//	10    Feeling sad or Tearful_encoded      -> target (solo para evaluar los clusters)
var featureColumns = [Dimensions]int{0, 2, 3, 4, 5, 6, 7, 8, 9}

const TargetColumn = 10

var featureNames = [Dimensions]string{"Age", "Irritable", "Trouble sleeping", "Concentrating",
	"Overeating/appetite", "Anxious", "Guilt", "Bonding with baby", "Suicide attempt"}

var targetLabels = [3]string{"No (0)", "Sometimes (0.5)", "Yes (1)"}

var riskLabels = [K]string{"Bajo", "Medio", "Alto"}

type Point []float64

// Resultado de una ejecución de K-means
type KMeansResult struct {
	Centroids  []Point
	Iterations int  // iteraciones ejecutadas
	Converged  bool // true si los centroides dejaron de moverse antes de MaxIters
	Duration   time.Duration
}

func distanceSq(p1, p2 Point) float64 {
	sum := 0.0
	for i := 0; i < len(p1); i++ {
		diff := p1[i] - p2[i]
		sum += diff * diff
	}
	return sum
}

func copyPoint(p Point) Point {
	c := make(Point, len(p))
	copy(c, p)
	return c
}

// Carga optimizada del CSV de 1M de registros sin paquetes de terceros.
// Devuelve las features para K-means, por separado el nivel del target (0, 1, 2) y
// cuántas filas se descartaron por tener columnas faltantes o valores inválidos.
//
// Todos los valores se guardan seguidos en un solo arreglo grande (values) y cada
// punto es un pedazo de ese arreglo: el punto i es values[i*Dimensions : (i+1)*Dimensions].
// Así los datos quedan contiguos en memoria y el procesador los lee en orden, lo que
// aprovecha mejor la caché que tener un millón de arreglos sueltos.
func loadDatasetFromCSV(filePath string) ([]Point, []int, int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, 0, err
	}
	defer file.Close()

	discarded := 0

	// Prealocar memoria para 1,000,000 de puntos
	values := make([]float64, 0, 1000000*Dimensions)
	targets := make([]int, 0, 1000000)
	point := make([]float64, Dimensions)           // fila actual mientras se valida
	reader := bufio.NewReaderSize(file, 1024*1024) // Buffer de 1 MB para lectura veloz

	isHeader := true

	for {
		line, err := reader.ReadString('\n')
		line = strings.TrimSpace(line)

		if len(line) > 0 {
			parts := strings.Split(line, ",")

			// Saltar encabezado si la primera columna no es numérica
			if isHeader {
				isHeader = false
				if _, errParse := strconv.ParseFloat(parts[0], 64); errParse != nil {
					if err != nil {
						break
					}
					continue
				}
			}

			if len(parts) <= TargetColumn {
				discarded++ // faltan columnas
			} else {
				valid := true
				for i := 0; i < Dimensions; i++ {
					val, parseErr := strconv.ParseFloat(strings.TrimSpace(parts[featureColumns[i]]), 64)
					if parseErr != nil {
						valid = false
						break
					}
					point[i] = val
				}
				// El target solo puede ser 0, 0.5 o 1 -> nivel 0, 1 o 2
				target, parseErr := strconv.ParseFloat(strings.TrimSpace(parts[TargetColumn]), 64)
				level := int(math.Round(target * 2))
				if parseErr != nil || level < 0 || level > 2 || float64(level) != target*2 {
					valid = false
				}
				if valid {
					values = append(values, point...)
					targets = append(targets, level)
				} else {
					discarded++ // algún valor no es un número o el target es inválido
				}
			}
		}

		// io.EOF significa que se terminó el archivo; cualquier otro error es un
		// problema de lectura y no se debe devolver un dataset incompleto como si nada
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, 0, err
		}
	}

	// Armar los puntos recién al final: si se armaran durante la carga, un append que
	// agranda 'values' lo movería a otra parte de la memoria y los puntos ya armados
	// quedarían apuntando al arreglo viejo
	n := len(targets)
	dataset := make([]Point, n)
	for i := 0; i < n; i++ {
		dataset[i] = values[i*Dimensions : (i+1)*Dimensions]
	}

	return dataset, targets, discarded, nil
}

// ==========================================
// FUNCIONES COMUNES DE K-MEANS
// ==========================================

// K-means++: el primer centroide es un punto al azar y cada uno de los siguientes se
// elige al azar dando más probabilidad a los puntos lejanos de los centroides ya
// elegidos (probabilidad proporcional a la distancia al cuadrado). Así los centroides
// iniciales quedan separados entre sí. Con la misma semilla siempre elige los mismos.
func initialCentroids(dataset []Point, seed int64) []Point {
	rng := rand.New(rand.NewSource(seed))
	centroids := make([]Point, 0, K)
	centroids = append(centroids, copyPoint(dataset[rng.Intn(len(dataset))]))

	// minDist[i] = distancia al cuadrado del punto i a su centroide elegido más cercano
	minDist := make([]float64, len(dataset))
	for i := 0; i < len(dataset); i++ {
		minDist[i] = distanceSq(dataset[i], centroids[0])
	}

	for len(centroids) < K {
		total := 0.0
		for i := 0; i < len(dataset); i++ {
			total += minDist[i]
		}

		// Elegir un punto: se toma un número al azar entre 0 y total y se recorren los
		// puntos acumulando sus distancias hasta superarlo
		r := rng.Float64() * total
		chosen := len(dataset) - 1
		accumulated := 0.0
		for i := 0; i < len(dataset); i++ {
			accumulated += minDist[i]
			if accumulated > r {
				chosen = i
				break
			}
		}
		newCentroid := copyPoint(dataset[chosen])
		centroids = append(centroids, newCentroid)

		// Actualizar la distancia de cada punto teniendo en cuenta el nuevo centroide
		for i := 0; i < len(dataset); i++ {
			d := distanceSq(dataset[i], newCentroid)
			if d < minDist[i] {
				minDist[i] = d
			}
		}
	}
	return centroids
}

// Devuelve el índice del centroide más cercano al punto p
func nearestCentroid(p Point, centroids []Point) int {
	bestCluster := 0
	minDist := math.MaxFloat64
	for c := 0; c < K; c++ {
		d := distanceSq(p, centroids[c])
		if d < minDist {
			minDist = d
			bestCluster = c
		}
	}
	return bestCluster
}

// Asigna cada punto a su centroide más cercano y devuelve, por cluster, la suma de
// sus puntos y cuántos puntos tiene. La versión secuencial la llama con todo el
// dataset y la concurrente la llama una vez por trabajador con su parte del dataset.
func assignAndSum(points []Point, centroids []Point) ([]Point, []int) {
	sums := make([]Point, K)
	for c := 0; c < K; c++ {
		sums[c] = make(Point, Dimensions)
	}
	counts := make([]int, K)

	for _, p := range points {
		c := nearestCentroid(p, centroids)
		counts[c]++
		for d := 0; d < Dimensions; d++ {
			sums[c][d] += p[d]
		}
	}
	return sums, counts
}

// Calcula los nuevos centroides (promedio de los puntos de cada cluster) y devuelve
// también el mayor desplazamiento al cuadrado, que se usa para saber si convergió.
func updateCentroids(oldCentroids []Point, sums []Point, counts []int) ([]Point, float64) {
	newCentroids := make([]Point, K)
	maxShift := 0.0
	for c := 0; c < K; c++ {
		if counts[c] == 0 {
			// Cluster vacío: no tiene puntos para promediar, así que conserva su
			// centroide anterior (antes quedaba en el origen, todo en 0)
			newCentroids[c] = copyPoint(oldCentroids[c])
			continue
		}
		newCentroids[c] = make(Point, Dimensions)
		for d := 0; d < Dimensions; d++ {
			newCentroids[c][d] = sums[c][d] / float64(counts[c])
		}
		shift := distanceSq(oldCentroids[c], newCentroids[c])
		if shift > maxShift {
			maxShift = shift
		}
	}
	return newCentroids, maxShift
}

// Inercia: suma de las distancias al cuadrado de cada punto a su centroide.
// Es lo que K-means minimiza: cuanto menor, más compactos son los clusters.
func inertia(dataset []Point, centroids []Point) float64 {
	total := 0.0
	for _, p := range dataset {
		c := nearestCentroid(p, centroids)
		total += distanceSq(p, centroids[c])
	}
	return total
}

// ==========================================
// 1. K-MEANS SECUENCIAL
// ==========================================
func KMeansSequential(dataset []Point, seed int64) KMeansResult {
	start := time.Now()
	result := KMeansResult{}
	centroids := initialCentroids(dataset, seed)

	for iter := 0; iter < MaxIters; iter++ {
		sums, counts := assignAndSum(dataset, centroids)

		newCentroids, maxShift := updateCentroids(centroids, sums, counts)
		centroids = newCentroids
		result.Iterations = iter + 1

		if maxShift < Threshold {
			result.Converged = true
			break
		}
	}

	result.Centroids = centroids
	result.Duration = time.Since(start)
	return result
}

// ==========================================
// 2. K-MEANS CONCURRENTE (WORKER POOL)
// ==========================================

// Cantidad de puntos de cada trabajo. Los bloques pequeños permiten que un núcleo
// rápido que termina antes tome más trabajo, en vez de esperar a los núcleos lentos.
// Es una variable (y no una constante) para que los tests puedan probar otros tamaños.
var ChunkSize = 10000

// Trabajo que el maestro envía a la cola: un rango del dataset y los centroides de la
// iteración actual (los trabajadores solo los leen, nunca los modifican)
type Job struct {
	Start     int
	End       int
	Centroids []Point
}

type PartialResult struct {
	Sums   []Point
	Counts []int
}

// Trabajador permanente: toma trabajos de la cola hasta que el maestro la cierra
func worker(dataset []Point, jobs <-chan Job, results chan<- PartialResult, wg *sync.WaitGroup) {
	defer wg.Done()
	for job := range jobs {
		sums, counts := assignAndSum(dataset[job.Start:job.End], job.Centroids)
		results <- PartialResult{Sums: sums, Counts: counts}
	}
}

func KMeansConcurrent(dataset []Point, numWorkers int, seed int64) KMeansResult {
	start := time.Now()
	result := KMeansResult{}
	centroids := initialCentroids(dataset, seed)

	// Con 0 trabajadores nadie procesaría la cola y el maestro esperaría para siempre
	if numWorkers < 1 {
		numWorkers = 1
	}

	n := len(dataset)
	numJobs := (n + ChunkSize - 1) / ChunkSize

	// Los canales tienen espacio para todos los trabajos de una iteración, así el
	// maestro puede repartirlos todos sin quedarse bloqueado
	jobs := make(chan Job, numJobs)
	results := make(chan PartialResult, numJobs)

	// Los trabajadores se crean una sola vez y sirven para todas las iteraciones
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go worker(dataset, jobs, results, &wg)
	}

	for iter := 0; iter < MaxIters; iter++ {
		// 1. Repartir: un trabajo por cada bloque de ChunkSize puntos
		for j := 0; j < numJobs; j++ {
			startIdx := j * ChunkSize
			endIdx := startIdx + ChunkSize
			if endIdx > n {
				endIdx = n
			}
			jobs <- Job{Start: startIdx, End: endIdx, Centroids: centroids}
		}

		// 2. Reducción: esperar un resultado por cada trabajo y juntarlos
		globalSums := make([]Point, K)
		for c := 0; c < K; c++ {
			globalSums[c] = make(Point, Dimensions)
		}
		globalCounts := make([]int, K)

		for j := 0; j < numJobs; j++ {
			res := <-results
			for c := 0; c < K; c++ {
				globalCounts[c] += res.Counts[c]
				for d := 0; d < Dimensions; d++ {
					globalSums[c][d] += res.Sums[c][d]
				}
			}
		}

		// 3. Actualizar: todos los trabajos de esta iteración ya terminaron, así que
		// ningún trabajador está leyendo los centroides que se reemplazan
		newCentroids, maxShift := updateCentroids(centroids, globalSums, globalCounts)
		centroids = newCentroids
		result.Iterations = iter + 1

		if maxShift < Threshold {
			result.Converged = true
			break
		}
	}

	// Cerrar la cola avisa a los trabajadores que terminen; se espera a que salgan
	close(jobs)
	wg.Wait()

	result.Centroids = centroids
	result.Duration = time.Since(start)
	return result
}

// ==========================================
// 3. NIVELES DE RIESGO Y EVALUACIÓN
// ==========================================

// Valor mínimo y máximo de cada columna del dataset (en valores normalizados)
func columnRanges(dataset []Point) (Point, Point) {
	mins := copyPoint(dataset[0])
	maxs := copyPoint(dataset[0])
	for _, p := range dataset {
		for d := 0; d < Dimensions; d++ {
			if p[d] < mins[d] {
				mins[d] = p[d]
			}
			if p[d] > maxs[d] {
				maxs[d] = p[d]
			}
		}
	}
	return mins, maxs
}

// Convierte el valor normalizado de un síntoma a su escala original (0 = No,
// 0.5 = respuesta intermedia, 1 = Yes). El z-score es una transformación lineal, así
// que el mínimo de la columna corresponde a 0, el máximo a 1 y el intermedio a 0.5.
func toOriginalScale(value, minValue, maxValue float64) float64 {
	return (value - minValue) / (maxValue - minValue)
}

// Severidad de un centroide: promedio de sus síntomas en la escala original 0-1 (sin
// la edad, que está en la posición 0). Se usa la escala original porque en la
// normalizada un síntoma poco frecuente toma valores más altos y pesaría más.
func severity(centroid Point, mins, maxs Point) float64 {
	sum := 0.0
	for d := 1; d < Dimensions; d++ {
		sum += toOriginalScale(centroid[d], mins[d], maxs[d])
	}
	return sum / float64(Dimensions-1)
}

// Ordena los centroides de menor a mayor severidad: 0 = Bajo, 1 = Medio, 2 = Alto
func sortBySeverity(centroids []Point, mins, maxs Point) []Point {
	sorted := make([]Point, K)
	copy(sorted, centroids)
	sort.Slice(sorted, func(i, j int) bool {
		return severity(sorted[i], mins, maxs) < severity(sorted[j], mins, maxs)
	})
	return sorted
}

// Predice el nivel de riesgo de un caso: el del centroide más cercano. Los centroides
// deben estar ordenados por severidad (sortBySeverity) para que el índice 0 sea Bajo,
// el 1 Medio y el 2 Alto. El caso debe venir normalizado igual que el dataset.
func predictRisk(p Point, sortedCentroids []Point) string {
	return riskLabels[nearestCentroid(p, sortedCentroids)]
}

// Muestra el perfil de síntomas de cada nivel de riesgo y cuántos registros de cada
// nivel del target ('Feeling sad', que no se usó en el clustering) cayeron en cada uno.
// La pureza es la fracción de registros que pertenece al nivel mayoritario de su
// cluster; se compara con la línea base (la proporción del nivel más frecuente).
// Recibe los centroides ya ordenados por severidad.
func printRiskLevels(dataset []Point, targets []int, centroids []Point, mins, maxs Point) {
	// Edad en z-score; síntomas en escala original (0 = No, 0.5 = intermedio, 1 = Yes)
	fmt.Println("Perfil de cada nivel de riesgo (centroides; síntomas en escala 0-1):")
	fmt.Printf("%-21s %8s %8s %8s\n", "Característica", riskLabels[0], riskLabels[1], riskLabels[2])
	fmt.Printf("%-21s %8.2f %8.2f %8.2f\n", "Age (z-score)", centroids[0][0], centroids[1][0], centroids[2][0])
	for d := 1; d < Dimensions; d++ {
		fmt.Printf("%-21s %8.2f %8.2f %8.2f\n", featureNames[d],
			toOriginalScale(centroids[0][d], mins[d], maxs[d]),
			toOriginalScale(centroids[1][d], mins[d], maxs[d]),
			toOriginalScale(centroids[2][d], mins[d], maxs[d]))
	}
	fmt.Printf("%-21s %8.2f %8.2f %8.2f\n\n", "Severidad (promedio)",
		severity(centroids[0], mins, maxs), severity(centroids[1], mins, maxs), severity(centroids[2], mins, maxs))

	var counts [K][3]int
	for i, p := range dataset {
		c := nearestCentroid(p, centroids)
		counts[c][targets[i]]++
	}

	fmt.Println("Niveles de riesgo vs target 'Feeling sad or Tearful' (no usado en el clustering):")
	fmt.Printf("%-10s %10s %16s %10s %10s\n", "Riesgo", targetLabels[0], targetLabels[1], targetLabels[2], "Total")
	majority := 0
	var levelTotals [3]int
	for c := 0; c < K; c++ {
		total, maxCount := 0, 0
		for t := 0; t < 3; t++ {
			total += counts[c][t]
			levelTotals[t] += counts[c][t]
			if counts[c][t] > maxCount {
				maxCount = counts[c][t]
			}
		}
		majority += maxCount
		fmt.Printf("%-10s %10d %16d %10d %10d\n", riskLabels[c], counts[c][0], counts[c][1], counts[c][2], total)
	}
	baseline := 0
	for _, n := range levelTotals {
		if n > baseline {
			baseline = n
		}
	}
	fmt.Printf("Pureza: %.3f (línea base sin clustering: %.3f)\n\n",
		float64(majority)/float64(len(dataset)), float64(baseline)/float64(len(dataset)))
}

func main() {
	// Ruta del CSV configurable: go run . -csv otro_archivo.csv
	csvPath := flag.String("csv", "dataset_1m.csv", "ruta del CSV con los datos normalizados")
	flag.Parse()

	fmt.Printf("Cargando dataset clínico desde %s...\n", *csvPath)
	t0 := time.Now()
	dataset, targets, discarded, err := loadDatasetFromCSV(*csvPath)
	if err != nil {
		fmt.Printf("Error critico al cargar el archivo CSV: %v\n", err)
		return
	}
	fmt.Printf("Carga completada en %v. Total registros: %d | Descartados: %d | Dimensiones: %d\n\n",
		time.Since(t0), len(dataset), discarded, Dimensions)

	// K-means necesita al menos K puntos para elegir los K centroides iniciales
	if len(dataset) < K {
		fmt.Printf("Error: se necesitan al menos %d registros válidos y hay %d\n", K, len(dataset))
		return
	}

	// Cada semilla de K-means++ puede llevar a una solución distinta: se prueban varias
	// (con la versión concurrente, que es más rápida) y se elige la de menor inercia
	fmt.Printf("Probando %d inicializaciones K-means++ (gana la de menor inercia)...\n", Restarts)
	bestSeed := int64(1)
	bestInertia := math.MaxFloat64
	for seed := int64(1); seed <= Restarts; seed++ {
		res := KMeansConcurrent(dataset, runtime.NumCPU(), seed)
		in := inertia(dataset, res.Centroids)
		fmt.Printf("  Semilla %d: inercia %.0f | iteraciones %d\n", seed, in, res.Iterations)
		if in < bestInertia {
			bestInertia = in
			bestSeed = seed
		}
	}
	fmt.Printf("Mejor semilla: %d\n\n", bestSeed)

	fmt.Println("Iniciando procesamiento secuencial...")
	seq := KMeansSequential(dataset, bestSeed)
	fmt.Printf("Tiempo Secuencial: %v | Iteraciones: %d | Convergió: %v | Inercia: %.0f\n\n",
		seq.Duration, seq.Iterations, seq.Converged, inertia(dataset, seq.Centroids))

	mins, maxs := columnRanges(dataset)
	riskCentroids := sortBySeverity(seq.Centroids, mins, maxs)
	printRiskLevels(dataset, targets, riskCentroids, mins, maxs)

	// Ejemplo de predicción: los primeros registros del dataset son casos reales
	fmt.Println("Ejemplo de predicción con los 5 primeros registros reales:")
	for i := 0; i < 5; i++ {
		fmt.Printf("  Registro %d -> riesgo %s\n", i+1, predictRisk(dataset[i], riskCentroids))
	}
	fmt.Println()

	fmt.Println("Iniciando procesamiento concurrente (Worker Pools)...")
	workerCounts := []int{2, 4, 8, 12, 16}
	for _, w := range workerCounts {
		conc := KMeansConcurrent(dataset, w, bestSeed)
		// Seconds() conserva los decimales (Milliseconds() los truncaba)
		speedup := seq.Duration.Seconds() / conc.Duration.Seconds()
		fmt.Printf("Trabajadores: %2d | Tiempo Concurrente: %v | Iteraciones: %d | Speedup: %.2fx\n",
			w, conc.Duration, conc.Iterations, speedup)
	}
	fmt.Println("\nNota: tiempos de una sola ejecución. Para medir con varias repeticiones usar")
	fmt.Println("los benchmarks: go test -bench . -run ^$ -benchtime 3x -count 10")
}
