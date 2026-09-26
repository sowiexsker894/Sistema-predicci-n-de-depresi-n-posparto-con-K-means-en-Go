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
	K          = 3        // 3 niveles de riesgo clínico (Bajo, Medio, Alto)
	Dimensions = 11       // 11 características clínicas sintomáticas
	Threshold  = 1e-4     // Tolerancia de convergencia
	MaxIters   = 15       // Máximo de iteraciones
)

type Point []float64

func distanceSq(p1, p2 Point) float64 {
	sum := 0.0
	for i := 0; i < len(p1); i++ {
		diff := p1[i] - p2[i]
		sum += diff * diff
	}
	return sum
}

// Carga optimizada del CSV de 1M de registros sin paquetes de terceros
func loadDatasetFromCSV(filePath string) ([]Point, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Prealocar memoria para 1,000,000 de puntos
	dataset := make([]Point, 0, 1000000)
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

			if len(parts) >= Dimensions {
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
				if valid {
					dataset = append(dataset, point)
				}
			}
		}

		if err != nil {
			break
		}
	}

	return dataset, nil
}

// ==========================================
// 1. K-MEANS SECUENCIAL
// ==========================================
func KMeansSequential(dataset []Point) ([]Point, time.Duration) {
	start := time.Now()
	centroids := make([]Point, K)
	for i := 0; i < K; i++ {
		centroids[i] = make(Point, Dimensions)
		copy(centroids[i], dataset[i])
	}

	for iter := 0; iter < MaxIters; iter++ {
		sums := make([]Point, K)
		counts := make([]int, K)
		for i := 0; i < K; i++ {
			sums[i] = make(Point, Dimensions)
		}

		for _, p := range dataset {
			bestCluster := 0
			minDist := math.MaxFloat64
			for c := 0; c < K; c++ {
				d := distanceSq(p, centroids[c])
				if d < minDist {
					minDist = d
					bestCluster = c
				}
			}
			counts[bestCluster]++
			for d := 0; d < Dimensions; d++ {
				sums[bestCluster][d] += p[d]
			}
		}

		maxShift := 0.0
		for c := 0; c < K; c++ {
			newC := make(Point, Dimensions)
			if counts[c] > 0 {
				for d := 0; d < Dimensions; d++ {
					newC[d] = sums[c][d] / float64(counts[c])
				}
			}
			shift := distanceSq(centroids[c], newC)
			if shift > maxShift {
				maxShift = shift
			}
			centroids[c] = newC
		}

		if maxShift < Threshold {
			break
		}
	}
	return centroids, time.Since(start)
}

// ==========================================
// 2. K-MEANS CONCURRENTE (WORKER POOL)
// ==========================================
type PartialResult struct {
	Sums   []Point
	Counts []int
}

func KMeansConcurrent(dataset []Point, numWorkers int) ([]Point, time.Duration) {
	start := time.Now()
	centroids := make([]Point, K)
	for i := 0; i < K; i++ {
		centroids[i] = make(Point, Dimensions)
		copy(centroids[i], dataset[i])
	}

	n := len(dataset)
	chunkSize := (n + numWorkers - 1) / numWorkers

	for iter := 0; iter < MaxIters; iter++ {
		resultsChan := make(chan PartialResult, numWorkers)
		var wg sync.WaitGroup

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
				localSums := make([]Point, K)
				localCounts := make([]int, K)
				for i := 0; i < K; i++ {
					localSums[i] = make(Point, Dimensions)
				}

				for _, p := range subDataset {
					bestCluster := 0
					minDist := math.MaxFloat64
					for c := 0; c < K; c++ {
						d := distanceSq(p, currentCentroids[c])
						if d < minDist {
							minDist = d
							bestCluster = c
						}
					}
					localCounts[bestCluster]++
					for d := 0; d < Dimensions; d++ {
						localSums[bestCluster][d] += p[d]
					}
				}

				resultsChan <- PartialResult{Sums: localSums, Counts: localCounts}
			}(dataset[startIdx:endIdx], centroids)
		}

		go func() {
			wg.Wait()
			close(resultsChan)
		}()

		globalSums := make([]Point, K)
		globalCounts := make([]int, K)
		for i := 0; i < K; i++ {
			globalSums[i] = make(Point, Dimensions)
		}

		for res := range resultsChan {
			for c := 0; c < K; c++ {
				globalCounts[c] += res.Counts[c]
				for d := 0; d < Dimensions; d++ {
					globalSums[c][d] += res.Sums[c][d]
				}
			}
		}

		maxShift := 0.0
		for c := 0; c < K; c++ {
			newC := make(Point, Dimensions)
			if globalCounts[c] > 0 {
				for d := 0; d < Dimensions; d++ {
					newC[d] = globalSums[c][d] / float64(globalCounts[c])
				}
			}
			shift := distanceSq(centroids[c], newC)
			if shift > maxShift {
				maxShift = shift
			}
			centroids[c] = newC
		}

		if maxShift < Threshold {
			break
		}
	}
	return centroids, time.Since(start)
}

func main() {
	csvPath := "dataset_1m.csv"
	fmt.Printf("Cargando dataset clínico desde %s...\n", csvPath)
	t0 := time.Now()
	dataset, err := loadDatasetFromCSV(csvPath)
	if err != nil {
		fmt.Printf("Error critico al cargar el archivo CSV: %v\n", err)
		return
	}
	fmt.Printf("Carga completada en %v. Total registros: %d | Dimensiones: %d\n\n", time.Since(t0), len(dataset), Dimensions)

	fmt.Println("Iniciando procesamiento secuencial...")
	_, seqTime := KMeansSequential(dataset)
	fmt.Printf("Tiempo Secuencial: %v\n\n", seqTime)

	fmt.Println("Iniciando procesamiento concurrente (Worker Pools)...")
	workerCounts := []int{2, 4, 8, 16}
	for _, w := range workerCounts {
		_, concTime := KMeansConcurrent(dataset, w)
		speedup := float64(seqTime.Milliseconds()) / float64(concTime.Milliseconds())
		fmt.Printf("Trabajadores: %2d | Tiempo Concurrente: %v | Speedup: %.2fx\n", w, concTime, speedup)
	}
}
