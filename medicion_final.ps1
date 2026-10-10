# Medicion final de rendimiento para el informe.
#
# Uso (desde la carpeta del proyecto, con dataset_1m.csv presente):
#   powershell -ExecutionPolicy Bypass -File .\medicion_final.ps1
#   powershell -ExecutionPolicy Bypass -File .\medicion_final.ps1 -Count 1   (prueba rapida)
#
# Antes de ejecutar: laptop conectada a la corriente, programas cerrados, modo de
# energia "Maximo rendimiento" y unos minutos sin uso para que el equipo se enfrie.
#
# Genera en la carpeta resultados/:
#   benchmark_<fecha>.txt  salida completa de go test -bench
#   benchmark_<fecha>.md   tabla con mediana, minimo, maximo, variacion y speedup

param(
    [int]$Count = 10   # mediciones por configuracion
)

$ErrorActionPreference = "Stop"

if (-not (Test-Path "dataset_1m.csv")) {
    Write-Host "Falta dataset_1m.csv en esta carpeta (ver README)." -ForegroundColor Red
    exit 1
}

New-Item -ItemType Directory -Force "resultados" | Out-Null
$stamp = Get-Date -Format "yyyyMMdd_HHmm"
$rawFile = "resultados\benchmark_$stamp.txt"
$mdFile = "resultados\benchmark_$stamp.md"

# 1. Condiciones de la medicion
$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$battery = Get-CimInstance Win32_Battery -ErrorAction SilentlyContinue
if ($battery -and $battery.BatteryStatus -eq 2) { $power = "conectada a la corriente" }
elseif ($battery) { $power = "con bateria (los tiempos pueden ser peores)" }
else { $power = "equipo de escritorio" }
$plan = (powercfg /getactivescheme) -replace '.*\((.*)\).*', '$1'
$goVersion = (go version)

Write-Host "CPU: $($cpu.Name) ($($cpu.NumberOfCores) nucleos, $($cpu.NumberOfLogicalProcessors) hilos)"
Write-Host "Energia: $power | Plan: $plan"
Write-Host "$goVersion"
Write-Host ""

# 2. Los tests deben pasar antes de medir
Write-Host "Ejecutando tests..."
go test -short .
if ($LASTEXITCODE -ne 0) {
    Write-Host "Los tests fallaron: no se mide." -ForegroundColor Red
    exit 1
}

# 3. Benchmarks: cada medicion promedia 3 ejecuciones; se repite $Count veces
Write-Host "Ejecutando benchmarks ($Count mediciones por configuracion, varios minutos)..."
go test -bench . -run '^$' -benchtime 3x -count $Count | Tee-Object -FilePath $rawFile
if ($LASTEXITCODE -ne 0) {
    Write-Host "Los benchmarks fallaron." -ForegroundColor Red
    exit 1
}

# 4. Agrupar los tiempos (en ms) por configuracion
$times = [ordered]@{}
foreach ($line in Get-Content $rawFile) {
    if ($line -match '^Benchmark(\S+?)-\d+\s+\d+\s+([\d.]+) ns/op') {
        $name = $Matches[1]
        $ms = [double]$Matches[2] / 1e6
        if (-not $times.Contains($name)) { $times[$name] = New-Object System.Collections.ArrayList }
        [void]$times[$name].Add($ms)
    }
}

function Get-Median($values) {
    $sorted = $values | Sort-Object
    $n = $sorted.Count
    if ($n % 2 -eq 1) { return $sorted[($n - 1) / 2] }
    return ($sorted[$n / 2 - 1] + $sorted[$n / 2]) / 2
}

function Get-StdDevPercent($values) {
    $mean = ($values | Measure-Object -Average).Average
    if ($values.Count -lt 2) { return 0 }
    $sum = 0.0
    foreach ($v in $values) { $sum += ($v - $mean) * ($v - $mean) }
    return [math]::Sqrt($sum / ($values.Count - 1)) / $mean * 100
}

$seqMedian = Get-Median $times["Sequential"]

# 5. Tabla en Markdown
$lines = @()
$lines += "# Medicion final de rendimiento"
$lines += ""
$lines += "- Fecha: $(Get-Date -Format 'yyyy-MM-dd HH:mm')"
$lines += "- CPU: $($cpu.Name) ($($cpu.NumberOfCores) nucleos, $($cpu.NumberOfLogicalProcessors) hilos)"
$lines += "- Energia: $power | Plan: $plan"
$lines += "- $goVersion"
$lines += "- Dataset: 1 000 000 registros, 9 dimensiones, K = 3, semilla 3 (16 iteraciones)"
$lines += "- $Count mediciones por configuracion; cada una promedia 3 ejecuciones"
$lines += ""
$lines += "| Configuracion | Mediana (ms) | Min (ms) | Max (ms) | Variacion | Speedup |"
$lines += "|---|---|---|---|---|---|"
foreach ($name in $times.Keys) {
    $values = $times[$name]
    $median = Get-Median $values
    $min = ($values | Measure-Object -Minimum).Minimum
    $max = ($values | Measure-Object -Maximum).Maximum
    $cv = Get-StdDevPercent $values
    $speedup = $seqMedian / $median
    $lines += "| $name | {0:0} | {1:0} | {2:0} | {3:0.0} % | {4:0.00}x |" -f $median, $min, $max, $cv, $speedup
}
$lines += ""
$lines += "Speedup = mediana secuencial / mediana de la configuracion."
$lines += "ChunkSize/chunk=N: 8 trabajadores con bloques de N puntos."

$lines | Out-File -FilePath $mdFile -Encoding utf8
Write-Host ""
Get-Content $mdFile
Write-Host ""
Write-Host "Resultados guardados en $rawFile y $mdFile" -ForegroundColor Green
