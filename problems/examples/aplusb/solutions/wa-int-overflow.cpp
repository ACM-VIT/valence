// Wrong: a and b each fit in a 32-bit int, but a + b can reach 4e9, which overflows it (test 04).
#include <iostream>

int main() {
    int a, b;
    std::cin >> a >> b;
    std::cout << a + b << "\n";
}
