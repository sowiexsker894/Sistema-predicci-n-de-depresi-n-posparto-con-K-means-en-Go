package main

import (
	"fmt"
	"math"
	"os"
	"testing"
)

// ==========================================
// TESTS
// ==========================================

// Crea un dataset pequeño con 3 grupos bien separados: todos los valores de un punto
// están cerca de 0, de 5 o de 10. Los 3 primeros puntos son de grupos distintos, así la
// inicialización (los K primeros puntos) empieza con un centroide en cada grupo.
// Como los desplazamientos suman 0, el centro exacto de cada grupo es 0, 5 y 10.
func makeTestDataset() []Point {
	groupCenters := []float64{0, 5, 10}
	offsets := []float64{-0.3, -0.1, 0.1, 0.3}

	var dataset []Point
	for _, offset := range offsets {
		for _, center := range groupCenters {
			p := make(Point, Dimensions)
			for d := 0; d < Dimensions; d++ {
				p[d] = center + offset
			}
			dataset = append(dataset, p)
		}
	}
	return dataset
}

// Compara dos listas de centroides y devuelve la mayor diferencia entre sus valores
func maxCentroidDiff(a, b []Point) float64 {
	maxDiff := 0.0
	for c := 0; c < K; c++ {
		for d := 0; d < Dimensions; d++ {
			diff := math.Abs(a[c][d] - b[c][d])
			if diff > maxDiff {
				maxDiff = diff
			}
		}
	}
	return maxDiff
}

func TestNearestCentroid(t *testing.T) {
	centroids := []Point{
		make(Point, Dimensions), // todo 0
		make(Point, Dimensions), // todo 5
		make(Point, Dimensions), // todo 10
	}
	for d := 0; d < Dimensions; d++ {
		centroids[1][d] = 5
		centroids[2][d] = 10
	}

	p := make(Point, Dimensions)
	for d := 0; d < Dimensions; d++ {
		p[d] = 6 // más cerca de 5 que de 0 o 10
	}

	if got := nearestCentroid(p, centroids); got != 1 {
		t.Errorf("nearestCentroid = %d, se esperaba 1", got)
	}
}

func TestSequentialFindsKnownClusters(t *testing.T) {
	result := KMeansSequential(makeTestDataset())

	if !result.Converged {
		t.Fatalf("no convergió en %d iteraciones", result.Iterations)
	}
	expected := []float64{0, 5, 10}
	for c := 0; c < K; c++ {
		for d := 0; d < Dimensions; d++ {
			if math.Abs(result.Centroids[c][d]-expected[c]) > 1e-9 {
				t.Fatalf("centroide %d = %v, se esperaba %v en todas las dimensiones",
					c, result.Centroids[c], expected[c])
			}
		}
	}
}

func TestConcurrentMatchesSequential(t *testing.T) {
	dataset := makeTestDataset()
	seq := KMeansSequential(dataset)

	// 20 trabajadores > 12 puntos: comprueba que no falle si sobran trabajadores
	for _, workers := range []int{1, 2, 3, 5, 12, 20} {
		conc := KMeansConcurrent(dataset, workers)
		if diff := maxCentroidDiff(seq.Centroids, conc.Centroids); diff > 1e-9 {
			t.Errorf("%d trabajadores: centroides distintos al secuencial (diferencia %.2e)", workers, diff)
		}
		if conc.Iterations != seq.Iterations {
			t.Errorf("%d trabajadores: %d iteraciones, el secuencial hizo %d", workers, conc.Iterations, seq.Iterations)
		}
	}
}

// Igual que el test anterior pero con el dataset real de 1M. Tarda unos segundos;
// se salta con 'go test -short' o si no está dataset_1m.csv.
func TestConcurrentMatchesSequentialRealData(t *testing.T) {
	if testing.Short() {
		t.Skip("se omite con -short")
	}
	if _, err := os.Stat("dataset_1m.csv"); err != nil {
		t.Skip("falta dataset_1m.csv (ver README)")
	}
	dataset, _, err := loadDatasetFromCSV("dataset_1m.csv")
	if err != nil {
		t.Fatal(err)
	}

	seq := KMeansSequential(dataset)
	if !seq.Converged {
		t.Errorf("el secuencial no convergió en %d iteraciones", seq.Iterations)
	}
	for _, workers := range []int{2, 8, 16} {
		conc := KMeansConcurrent(dataset, workers)
		// Tolerancia pequeña: sumar en distinto orden cambia los últimos decimales
		if diff := maxCentroidDiff(seq.Centroids, conc.Centroids); diff > 1e-9 {
			t.Errorf("%d trabajadores: centroides distintos al secuencial (diferencia %.2e)", workers, diff)
		}
	}
}

// ==========================================
// BENCHMARKS
// ==========================================
// Ejecutar con: go test -bench . -run ^$ -benchtime 3x -count 10
// (-run ^$ evita ejecutar los tests; cada medición promedia 3 ejecuciones y se repite 10 veces)

var benchDataset []Point

// Carga dataset_1m.csv una sola vez y lo reutiliza en todos los benchmarks
func loadBenchDataset(b *testing.B) []Point {
	if benchDataset == nil {
		if _, err := os.Stat("dataset_1m.csv"); err != nil {
			b.Skip("falta dataset_1m.csv (ver README)")
		}
		dataset, _, err := loadDatasetFromCSV("dataset_1m.csv")
		if err != nil {
			b.Fatal(err)
		}
		benchDataset = dataset
	}
	return benchDataset
}

func BenchmarkSequential(b *testing.B) {
	dataset := loadBenchDataset(b)
	b.ResetTimer() // no contar el tiempo de carga del CSV
	for i := 0; i < b.N; i++ {
		KMeansSequential(dataset)
	}
}

func BenchmarkConcurrent(b *testing.B) {
	dataset := loadBenchDataset(b)
	for _, workers := range []int{2, 4, 8, 12, 16} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				KMeansConcurrent(dataset, workers)
			}
		})
	}
}
