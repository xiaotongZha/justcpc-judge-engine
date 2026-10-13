#include <fstream>
#include <iostream>

int main() {
    std::ifstream host_file("/home/zhaxt/JustCPC/JustJudge/CodeEvalutor.py");
    if (host_file.good()) {
        std::cout << "host file was visible\n";
        return 10;
    }
    std::cout << "host file is not visible\n";
    return 0;
}
