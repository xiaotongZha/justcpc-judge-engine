#include <unistd.h>

int main() {
    char block[1024] = {};
    for (;;) {
        write(STDOUT_FILENO, block, sizeof(block));
    }
}
