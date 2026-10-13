#include <sys/ptrace.h>

int main() {
    return ptrace(PTRACE_TRACEME, 0, nullptr, nullptr);
}
