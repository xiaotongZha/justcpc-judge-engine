#include <iostream>

struct Node {
    int child[12];
    int parent;
    int value;
    int subtree_size;
    int lazy_tag;
};

// A trie/tree-style data structure with five million nodes. Each node is
// 64 bytes, so the array uses about 305 MiB after every page is touched.
static volatile Node nodes[5000000];

int main() {
    for (int i = 0; i < 5000000; ++i) {
        nodes[i].child[0] = i;
    }
    std::cout << nodes[4999999].child[0] << '\n';
    return 0;
}
