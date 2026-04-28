//go:build ignore

package main

import "fmt"

func main() {
	res := make([]int, 3)
	for i := 0; i < 3; i++ {
		fmt.Scanf("%d", &res)
	}
	fmt.Println(res)

}
