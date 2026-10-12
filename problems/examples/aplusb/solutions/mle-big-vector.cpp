// Allocates and touches 512 MiB, over the 256 MB limit: expected verdict MLE.
#include <iostream>
#include <vector>

int main() {
    long long a, b;
    std::cin >> a >> b;
    std::vector<char> v(512 << 20);
    // Volatile writes so the compiler cannot drop the allocation; one per page
    // is enough to make the kernel back all 512 MiB with real memory.
    volatile char* p = v.data();
    for (std::size_t i = 0; i < v.size(); i += 4096) p[i] = 1;
    std::cout << a + b << "\n";
}
