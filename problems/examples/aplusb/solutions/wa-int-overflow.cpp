// Wrong: a + b can reach 2e9, which overflows a 32-bit int (test 04).
#include <iostream>

int main() {
    int a, b;
    std::cin >> a >> b;
    std::cout << a + b << "\n";
}
