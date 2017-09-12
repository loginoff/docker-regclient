package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

func Confirm(prompt string) bool {
	for {
		reader := bufio.NewReader(os.Stdin)
		fmt.Printf(prompt)
		ans, _ := reader.ReadString('\n')
		switch strings.TrimSpace(ans) {
		case "y":
			return true
		case "n":
			return false
		}
	}
}
