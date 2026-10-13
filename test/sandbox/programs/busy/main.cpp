#include <iostream>

int main() {
    // A typical over-complex contest solution: one billion dependent modular
    // transitions. The dependency prevents the compiler from collapsing it.
    volatile unsigned long long sum = 1;
    for (unsigned long long i = 0; i < 1000000000ULL; ++i) {
        sum = (sum * 48271ULL + i) % 2147483647ULL;
    }
    std::cout << sum << '\n';
    return 0;
}
