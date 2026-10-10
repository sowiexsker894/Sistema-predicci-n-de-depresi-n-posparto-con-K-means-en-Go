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
// están cerca de 0, de 5 o de 10. Como los desplazamientos suman 0, el centro exacto
// de cada grupo es 0, 5 y 10.
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

func TestLoadDatasetFromCSV(t *testing.T) {
	// CSV pequeño: encabezado, 2 filas válidas y 3 inválidas (target 0.3, un valor que
	// no es número y una fila con columnas faltantes).
	// Columnas: Age, sad, 8 síntomas, target (11 en total)
	content := "Age,sad,s1,s2,s3,s4,s5,s6,s7,s8,target\n" +
		"1,99,2,3,4,5,6,7,8,9,1.0\n" +
		"10,99,20,30,40,50,60,70,80,90,0.3\n" +
		"10,99,abc,30,40,50,60,70,80,90,1.0\n" +
		"10,99,20,30\n" +
		"-1,99,-2,-3,-4,-5,-6,-7,-8,-9,0.5\n"
	path := t.TempDir() + "/test.csv"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	dataset, targets, discarded, err := loadDatasetFromCSV(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(dataset) != 2 || len(targets) != 2 {
		t.Fatalf("se esperaban 2 filas válidas, se cargaron %d", len(dataset))
	}
	if discarded != 3 {
		t.Errorf("se esperaban 3 filas descartadas, se contaron %d", discarded)
	}

	// La columna 'sad' (99) no debe aparecer en los puntos
	expected := []Point{
		{1, 2, 3, 4, 5, 6, 7, 8, 9},
		{-1, -2, -3, -4, -5, -6, -7, -8, -9},
	}
	for i := 0; i < 2; i++ {
		for d := 0; d < Dimensions; d++ {
			if dataset[i][d] != expected[i][d] {
				t.Fatalf("punto %d = %v, se esperaba %v", i, dataset[i], expected[i])
			}
		}
	}
	if targets[0] != 2 || targets[1] != 1 {
		t.Errorf("targets = %v, se esperaba [2 1]", targets)
	}
}

func TestLoadDatasetMissingFile(t *testing.T) {
	_, _, _, err := loadDatasetFromCSV(t.TempDir() + "/no_existe.csv")
	if err == nil {
		t.Error("se esperaba un error al abrir un archivo que no existe")
	}
}

func TestUpdateCentroidsEmptyCluster(t *testing.T) {
	old := []Point{make(Point, Dimensions), make(Point, Dimensions), make(Point, Dimensions)}
	sums := []Point{make(Point, Dimensions), make(Point, Dimensions), make(Point, Dimensions)}
	for d := 0; d < Dimensions; d++ {
		old[2][d] = 7  // centroide anterior del cluster 2
		sums[0][d] = 4 // cluster 0: 2 puntos que suman 4 -> promedio 2
	}
	counts := []int{2, 0, 0} // los clusters 1 y 2 quedaron vacíos

	updated, _ := updateCentroids(old, sums, counts)
	if updated[0][0] != 2 {
		t.Errorf("cluster 0 = %v, se esperaba 2", updated[0][0])
	}
	if updated[2][0] != 7 {
		t.Errorf("un cluster vacío debe conservar su centroide anterior (7), quedó en %v", updated[2][0])
	}
}

func TestConcurrentZeroWorkers(t *testing.T) {
	// No debe quedarse bloqueado: con 0 trabajadores se usa 1
	dataset := makeTestDataset()
	seq := KMeansSequential(dataset, DefaultSeed)
	conc := KMeansConcurrent(dataset, 0, DefaultSeed)
	if diff := maxCentroidDiff(seq.Centroids, conc.Centroids); diff > 1e-9 {
		t.Errorf("centroides distintos al secuencial (diferencia %.2e)", diff)
	}
}

func TestPredictRisk(t *testing.T) {
	dataset := makeTestDataset()
	result := KMeansSequential(dataset, DefaultSeed)
	mins, maxs := columnRanges(dataset)
	riskCentroids := sortBySeverity(result.Centroids, mins, maxs)

	// Casos nuevos cerca de 0 (Bajo), 5 (Medio) y 10 (Alto)
	cases := []struct {
		value    float64
		expected string
	}{
		{0.2, "Bajo"},
		{4.8, "Medio"},
		{9.9, "Alto"},
	}
	for _, tc := range cases {
		p := make(Point, Dimensions)
		for d := 0; d < Dimensions; d++ {
			p[d] = tc.value
		}
		if got := predictRisk(p, riskCentroids); got != tc.expected {
			t.Errorf("predictRisk(%v) = %s, se esperaba %s", tc.value, got, tc.expected)
		}
	}
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

func TestInitialCentroidsSameSeedSameResult(t *testing.T) {
	dataset := makeTestDataset()
	a := initialCentroids(dataset, DefaultSeed)
	b := initialCentroids(dataset, DefaultSeed)
	if diff := maxCentroidDiff(a, b); diff != 0 {
		t.Errorf("con la misma semilla K-means++ eligió centroides distintos (diferencia %.2e)", diff)
	}
}

func TestSequentialFindsKnownClusters(t *testing.T) {
	// Se prueban varias semillas: con grupos tan separados todas deben encontrarlos
	for seed := int64(1); seed <= 5; seed++ {
		result := KMeansSequential(makeTestDataset(), seed)
		if !result.Converged {
			t.Fatalf("semilla %d: no convergió en %d iteraciones", seed, result.Iterations)
		}

		// K-means++ puede devolver los clusters en cualquier orden: se ordenan por severidad
		mins, maxs := columnRanges(makeTestDataset())
		centroids := sortBySeverity(result.Centroids, mins, maxs)
		expected := []float64{0, 5, 10}
		for c := 0; c < K; c++ {
			for d := 0; d < Dimensions; d++ {
				if math.Abs(centroids[c][d]-expected[c]) > 1e-9 {
					t.Fatalf("semilla %d: centroide %d = %v, se esperaba %v en todas las dimensiones",
						seed, c, centroids[c], expected[c])
				}
			}
		}
	}
}

func TestSortBySeverity(t *testing.T) {
	// La edad (posición 0) no debe influir: el primer centroide tiene la mayor edad
	// pero los síntomas más bajos, así que debe quedar como riesgo Bajo
	low := make(Point, Dimensions)
	medium := make(Point, Dimensions)
	high := make(Point, Dimensions)
	low[0] = 100
	for d := 1; d < Dimensions; d++ {
		low[d] = -1
		medium[d] = 0
		high[d] = 1
	}

	// Rango de cada columna: de -1 (No) a 1 (Yes); la edad de 0 a 100
	mins := make(Point, Dimensions)
	maxs := make(Point, Dimensions)
	maxs[0] = 100
	for d := 1; d < Dimensions; d++ {
		mins[d] = -1
		maxs[d] = 1
	}

	sorted := sortBySeverity([]Point{high, low, medium}, mins, maxs)
	if sorted[0][1] != -1 || sorted[1][1] != 0 || sorted[2][1] != 1 {
		t.Errorf("orden incorrecto: %v", sorted)
	}
}

func TestToOriginalScale(t *testing.T) {
	// Valores normalizados reales de 'Irritable' (No, Sometimes, Yes)
	minValue, maxValue := -1.230936, 1.151674
	middle := (minValue + maxValue) / 2

	tests := []struct {
		value    float64
		expected float64
	}{
		{minValue, 0},
		{middle, 0.5},
		{maxValue, 1},
	}
	for _, tc := range tests {
		if got := toOriginalScale(tc.value, minValue, maxValue); math.Abs(got-tc.expected) > 1e-9 {
			t.Errorf("toOriginalScale(%v) = %v, se esperaba %v", tc.value, got, tc.expected)
		}
	}
}

func TestConcurrentMatchesSequential(t *testing.T) {
	dataset := makeTestDataset()
	seq := KMeansSequential(dataset, DefaultSeed)

	// 20 trabajadores > 12 puntos: comprueba que no falle si sobran trabajadores
	for _, workers := range []int{1, 2, 3, 5, 12, 20} {
		conc := KMeansConcurrent(dataset, workers, DefaultSeed)
		if diff := maxCentroidDiff(seq.Centroids, conc.Centroids); diff > 1e-9 {
			t.Errorf("%d trabajadores: centroides distintos al secuencial (diferencia %.2e)", workers, diff)
		}
		if conc.Iterations != seq.Iterations {
			t.Errorf("%d trabajadores: %d iteraciones, el secuencial hizo %d", workers, conc.Iterations, seq.Iterations)
		}
	}
}

// Con bloques de 5 puntos, los 12 puntos se reparten en 3 trabajos (5 + 5 + 2):
// comprueba que la cola de trabajos y el último bloque incompleto funcionen bien
func TestConcurrentSmallChunks(t *testing.T) {
	oldChunkSize := ChunkSize
	ChunkSize = 5
	defer func() { ChunkSize = oldChunkSize }()

	dataset := makeTestDataset()
	seq := KMeansSequential(dataset, DefaultSeed)
	for _, workers := range []int{1, 2, 3, 8} {
		conc := KMeansConcurrent(dataset, workers, DefaultSeed)
		if diff := maxCentroidDiff(seq.Centroids, conc.Centroids); diff > 1e-9 {
			t.Errorf("%d trabajadores: centroides distintos al secuencial (diferencia %.2e)", workers, diff)
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
	dataset, _, _, err := loadDatasetFromCSV("dataset_1m.csv")
	if err != nil {
		t.Fatal(err)
	}

	seq := KMeansSequential(dataset, DefaultSeed)
	if !seq.Converged {
		t.Errorf("el secuencial no convergió en %d iteraciones", seq.Iterations)
	}
	for _, workers := range []int{2, 8, 16} {
		conc := KMeansConcurrent(dataset, workers, DefaultSeed)
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
		dataset, _, _, err := loadDatasetFromCSV("dataset_1m.csv")
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
		KMeansSequential(dataset, DefaultSeed)
	}
}

func BenchmarkConcurrent(b *testing.B) {
	dataset := loadBenchDataset(b)
	for _, workers := range []int{2, 4, 8, 12, 16} {
		b.Run(fmt.Sprintf("workers=%d", workers), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				KMeansConcurrent(dataset, workers, DefaultSeed)
			}
		})
	}
}

// Compara tamaños de bloque con 8 trabajadores: bloques muy chicos generan muchos
// mensajes por los canales y bloques muy grandes reparten peor el trabajo
func BenchmarkChunkSize(b *testing.B) {
	dataset := loadBenchDataset(b)
	oldChunkSize := ChunkSize
	defer func() { ChunkSize = oldChunkSize }()

	for _, size := range []int{1000, 10000, 50000, 125000} {
		ChunkSize = size
		b.Run(fmt.Sprintf("chunk=%d", size), func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				KMeansConcurrent(dataset, 8, DefaultSeed)
			}
		})
	}
}
