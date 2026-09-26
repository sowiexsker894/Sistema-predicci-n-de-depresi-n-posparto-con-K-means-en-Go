#define NUM_WORKERS 3
#define MAX_ITERS 2

chan to_worker[NUM_WORKERS] = [0] of { byte };
chan to_master = [0] of { byte, byte };

byte active_writers = 0;

proctype Worker(byte id) {
    byte iter;
    do
    :: to_worker[id] ? iter ->
        if
        :: iter == 255 -> break;
        :: else ->
            to_master ! id, 1;
        fi
    od
}

proctype Master() {
    byte iter = 0;
    byte w_id;
    byte status;
    byte count;

    do
    :: iter < MAX_ITERS ->
                                count = 0;
        do
        :: count < NUM_WORKERS ->
            to_worker[count] ! iter;
            count++;
        :: count == NUM_WORKERS -> break;
        od;

        count = 0;
        do
        :: count < NUM_WORKERS ->
            to_master ? w_id, status;
            active_writers++;
            assert(active_writers == 1);
            active_writers--;
            count++;
        :: count == NUM_WORKERS -> break;
        od;

        iter++;
    :: iter == MAX_ITERS -> break;
    od;

    count = 0;
    do
    :: count < NUM_WORKERS ->
        to_worker[count] ! 255;
        count++;
    :: count == NUM_WORKERS -> break;
    od;
}

init {
    byte i = 0;
    atomic {
        run Master();
        do
        :: i < NUM_WORKERS ->
            run Worker(i);
            i++;
        :: i == NUM_WORKERS -> break;
        od;
    }
}
