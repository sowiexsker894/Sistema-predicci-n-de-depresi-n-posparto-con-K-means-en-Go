package main

import (
	"bufio"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	K          = 3    // 3 niveles de riesgo clínico (Bajo, Medio, Alto)
	Dimensions = 10   // 10 características: Age + 9 síntomas (sin el target)
	Threshold  = 1e-4 // Tolerancia de convergencia (se compara con la distancia al cuadrado)
	MaxIters   = 100  // Máximo de iteraciones (con estos datos converge en unas 16)
)

// La columna 11 del CSV ('Feeling sad or Tearful_encoded') es el target: duplica la
// columna 2 y no entra al clustering, solo se usa para evaluar los clusters.
const TargetColumn = Dimensions

var targetLabels = [3]string{"No (0)", "Sometimes (0.5)", "Yes (1)"}

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

// Carga optimizada del CSV de 1M de registros sin paquetes de terceros.
// Devuelve las features para K-means y, por separado, el nivel del target (0, 1, 2).
func loadDatasetFromCSV(filePath string) ([]Point, []int, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, nil, err
	}
	defer file.Close()

	// Prealocar memoria para 1,000,000 de puntos
	dataset := make([]Point, 0, 1000000)
	targets := make([]int, 0, 1000000)
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

			if len(parts) > TargetColumn {
				point := make(Point, Dimensions)
				valid := true
				for i := 0; i < Dimensions; i++ {
					val, parseErr := strconv.ParseFloat(strings.TrimSpace(parts[i]), 64)
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
					dataset = append(dataset, point)
					targets = append(targets, level)
				}
			}
		}

		if err != nil {
			break
		}
	}

	return dataset, targets, nil
}

// ==========================================
// FUNCIONES COMUNES DE K-MEANS
// ==========================================

// Usa los K primeros puntos del dataset como centroides iniciales
func initialCentroids(dataset []Point) []Point {
	centroids := make([]Point, K)
	for i := 0; i < K; i++ {
		centroids[i] = make(Point, Dimensions)
		copy(centroids[i], dataset[i])
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
		newCentroids[c] = make(Point, Dimensions)
		if counts[c] > 0 {
			for d := 0; d < Dimensions; d++ {
				newCentroids[c][d] = sums[c][d] / float64(counts[c])
			}
		}
		shift := distanceSq(oldCentroids[c], newCentroids[c])
		if shift > maxShift {
			maxShift = shift
		}
	}
	return newCentroids, maxShift
}

// ==========================================
// 1. K-MEANS SECUENCIAL
// ==========================================
func KMeansSequential(dataset []Point) KMeansResult {
	start := time.Now()
	result := KMeansResult{}
	centroids := initialCentroids(dataset)

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
type PartialResult struct {
	Sums   []Point
	Counts []int
}

func KMeansConcurrent(dataset []Point, numWorkers int) KMeansResult {
	start := time.Now()
	result := KMeansResult{}
	centroids := initialCentroids(dataset)

	n := len(dataset)
	chunkSize := (n + numWorkers - 1) / numWorkers

	for iter := 0; iter < MaxIters; iter++ {
		resultsChan := make(chan PartialResult, numWorkers)
		var wg sync.WaitGroup

		// Cada trabajador procesa su parte del dataset con los centroides actuales
		for w := 0; w < numWorkers; w++ {
			startIdx := w * chunkSize
			endIdx := startIdx + chunkSize
			if startIdx >= n {
				break
			}
			if endIdx > n {
				endIdx = n
			}

			wg.Add(1)
			go func(subDataset []Point, currentCentroids []Point) {
				defer wg.Done()
				sums, counts := assignAndSum(subDataset, currentCentroids)
				resultsChan <- PartialResult{Sums: sums, Counts: counts}
			}(dataset[startIdx:endIdx], centroids)
		}

		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		// Reducción: juntar los resultados parciales de todos los trabajadores
		globalSums := make([]Point, K)
		for c := 0; c < K; c++ {
			globalSums[c] = make(Point, Dimensions)
		}
		globalCounts := make([]int, K)

		for res := range resultsChan {
			for c := 0; c < K; c++ {
				globalCounts[c] += res.Counts[c]
				for d := 0; d < Dimensions; d++ {
					globalSums[c][d] += res.Sums[c][d]
				}
			}
		}

		newCentroids, maxShift := updateCentroids(centroids, globalSums, globalCounts)
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
// 3. EVALUACIÓN: CLUSTERS VS TARGET
// ==========================================
// Asigna cada punto a su centroide más cercano y muestra cuántos registros de cada
// nivel del target cayeron en cada cluster. La pureza es la fracción de registros que
// pertenece al nivel mayoritario de su cluster; se compara con la línea base (la
// proporción del nivel más frecuente), que es la pureza de clusters que no separan nada.
func printClusterVsTarget(dataset []Point, targets []int, centroids []Point) {
	var counts [K][3]int
	for i, p := range dataset {
		c := nearestCentroid(p, centroids)
		counts[c][targets[i]]++
	}

	fmt.Println("Clusters vs target 'Feeling sad or Tearful' (no usado en el clustering):")
	fmt.Printf("%-10s %10s %16s %10s %10s\n", "Cluster", targetLabels[0], targetLabels[1], targetLabels[2], "Total")
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
		fmt.Printf("%-10d %10d %16d %10d %10d\n", c, counts[c][0], counts[c][1], counts[c][2], total)
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
	csvPath := "dataset_1m.csv"
	fmt.Printf("Cargando dataset clínico desde %s...\n", csvPath)
	t0 := time.Now()
	dataset, targets, err := loadDatasetFromCSV(csvPath)
	if err != nil {
		fmt.Printf("Error critico al cargar el archivo CSV: %v\n", err)
		return
	}
	fmt.Printf("Carga completada en %v. Total registros: %d | Dimensiones: %d\n\n", time.Since(t0), len(dataset), Dimensions)

	fmt.Println("Iniciando procesamiento secuencial...")
	seq := KMeansSequential(dataset)
	fmt.Printf("Tiempo Secuencial: %v | Iteraciones: %d | Convergió: %v\n\n", seq.Duration, seq.Iterations, seq.Converged)

	printClusterVsTarget(dataset, targets, seq.Centroids)

	fmt.Println("Iniciando procesamiento concurrente (Worker Pools)...")
	workerCounts := []int{2, 4, 8, 12, 16}
	for _, w := range workerCounts {
		conc := KMeansConcurrent(dataset, w)
		// Seconds() conserva los decimales (Milliseconds() los truncaba)
		speedup := seq.Duration.Seconds() / conc.Duration.Seconds()
		fmt.Printf("Trabajadores: %2d | Tiempo Concurrente: %v | Iteraciones: %d | Speedup: %.2fx\n",
			w, conc.Duration, conc.Iterations, speedup)
	}
	fmt.Println("\nNota: tiempos de una sola ejecución. Para medir con varias repeticiones usar")
	fmt.Println("los benchmarks: go test -bench . -run ^$ -benchtime 3x -count 10")
}
