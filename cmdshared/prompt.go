package cmdshared

import (
	"bufio"
	"fmt"
	"github.com/spf13/viper"
	"os"
	"strings"
)

func PromptYesNo(prompt string) bool {
	fmt.Print(prompt)
	if viper.GetBool("non-interactive") {
		fmt.Println("Y（非交互模式）")
		return true
	}
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		fmt.Printf("提示用户失败：%v\n", err)
		os.Exit(1)
	}

	ansNormal := strings.ToLower(strings.TrimSpace(answer))
	if len(ansNormal) > 0 && ansNormal[0] == 'n' {
		return false
	}
	return true
}
