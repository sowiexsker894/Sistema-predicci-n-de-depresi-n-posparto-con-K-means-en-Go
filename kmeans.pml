/*
 * Modelo Promela del K-means concurrente de main.go (funcion KMeansConcurrent).
 *
 * Representa el worker pool real del programa en Go:
 *   - un MAESTRO que en cada iteracion pone NUM_JOBS trabajos en la cola 'jobs',
 *     espera NUM_JOBS resultados en 'results' y luego actualiza los centroides;
 *   - NUM_WORKERS TRABAJADORES permanentes que toman trabajos de la cola, leen los
 *     centroides (assignAndSum) y envian su resultado parcial;
 *   - al final el maestro cierra la cola (close(jobs)) y espera a que todos los
 *     trabajadores terminen (wg.Wait()).
 *
 * Los datos (puntos, sumas) no se modelan: solo importa la sincronizacion.
 * Los centroides se representan por su numero de version (la iteracion a la que
 * pertenecen).
 *
 * Propiedades verificadas:
 *   1. Ausencia de deadlocks: todos los procesos terminan (sin estados finales
 *      invalidos).
 *   2. Exclusion mutua: los centroides nunca se actualizan mientras un trabajador
 *      los esta leyendo, y cada trabajador lee los centroides de la iteracion de
 *      su trabajo (asserts).
 *   3. Terminacion: el maestro siempre termina (formula LTL 'termina').
 *
 * Con -DERROR_SIN_ESPERA se compila una version con un error a proposito: el
 * maestro actualiza los centroides sin esperar el ultimo resultado. Spin debe
 * encontrar la violacion (sirve para mostrar que la verificacion detecta errores).
 */

#define NUM_WORKERS 3
#define NUM_JOBS    4   /* trabajos por iteracion (bloques de ChunkSize puntos) */
#define MAX_ITERS   2

mtype = { JOB, STOP };

/* Canales con buffer, igual que en Go: make(chan Job, numJobs) */
chan jobs    = [NUM_JOBS] of { mtype, byte };  /* tipo de mensaje, iteracion */
chan results = [NUM_JOBS] of { byte };         /* iteracion del resultado    */

byte centroidsVersion = 0;   /* iteracion de los centroides actuales            */
byte readers = 0;            /* trabajadores leyendo los centroides en este momento */
bool updating = false;       /* true mientras el maestro actualiza los centroides */
byte finished = 0;           /* trabajadores que ya salieron (contador del WaitGroup) */
bool masterDone = false;

/* func worker(...) en main.go */
proctype Worker() {
    mtype kind;
    byte jobIter;

    do
    :: jobs ? kind, jobIter ->
        if
        :: kind == STOP -> break   /* la cola se cerro: el trabajador termina */
        :: kind == JOB ->
            /* assignAndSum: leer los centroides */
            atomic { assert(!updating); readers++ };
            assert(jobIter == centroidsVersion);  /* lee los de su iteracion */
            readers--;
            results ! jobIter                     /* enviar el resultado parcial */
        fi
    od;
    finished++   /* defer wg.Done() */
}

/* El bucle de iteraciones de KMeansConcurrent en main.go */
proctype Master() {
    byte iter = 0;
    byte j;
    byte resIter;

    do
    :: iter < MAX_ITERS ->
        /* 1. Repartir: un trabajo por bloque */
        j = 0;
        do
        :: j < NUM_JOBS -> jobs ! JOB, iter; j++
        :: else -> break
        od;

        /* 2. Reduccion: esperar un resultado por cada trabajo */
        j = 0;
        do
#ifdef ERROR_SIN_ESPERA
        :: j < NUM_JOBS - 1 ->   /* ERROR: no espera el ultimo resultado */
#else
        :: j < NUM_JOBS ->
#endif
            results ? resIter;
            assert(resIter == iter);   /* no se mezclan resultados de iteraciones */
            j++
        :: else -> break
        od;

        /* 3. Actualizar los centroides: ningun trabajador puede estar leyendolos */
        updating = true;
        assert(readers == 0);
        centroidsVersion++;
        updating = false;

        /* Puede converger en cualquier iteracion o seguir hasta MAX_ITERS */
        if
        :: iter++        /* no convergio: otra iteracion */
        :: break         /* convergio (maxShift < Threshold) */
        fi
    :: else -> break
    od;

    /* close(jobs): un aviso STOP por trabajador, despues de todos los trabajos */
    j = 0;
    do
    :: j < NUM_WORKERS -> jobs ! STOP, 0; j++
    :: else -> break
    od;

    /* wg.Wait(): esperar a que todos los trabajadores terminen */
    (finished == NUM_WORKERS);
    masterDone = true
}

init {
    byte i = 0;
    atomic {
        do
        :: i < NUM_WORKERS -> run Worker(); i++
        :: else -> break
        od;
        run Master()
    }
}

/* Terminacion: en toda ejecucion el maestro llega al final */
ltl termina { <> masterDone }
